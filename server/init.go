package server

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/server/wslogs"
	"github.com/teranos/QNTX/graph"
	"github.com/teranos/QNTX/logger"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/budget"
	"github.com/teranos/QNTX/pulse/schedule"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// serverDependencies holds all dependencies created for QNTXServer
type serverDependencies struct {
	builder       *graph.AxGraphBuilder
	// langService   *lsp.Service          // TODO: Extract ats/lsp
	// usageTracker  *tracker.UsageTracker // TODO: Extract ai/tracker
	budgetTracker *budget.Tracker
	daemon        *async.WorkerPool
	config        *appcfg.Config // GRACE Phase 2 optimization: reuse for daemon recreation
}

// NewQNTXServer creates a new QNTX server
func NewQNTXServer(db *sql.DB, dbPath string, verbosity int) (*QNTXServer, error) {
	return NewQNTXServerWithInitialQuery(db, dbPath, verbosity, "")
}

// NewQNTXServerWithInitialQuery creates a QNTXServer with an optional pre-loaded Ax query
func NewQNTXServerWithInitialQuery(db *sql.DB, dbPath string, verbosity int, initialQuery string) (*QNTXServer, error) {
	// Defensive: Validate critical inputs
	if db == nil {
		return nil, fmt.Errorf("database connection cannot be nil")
	}
	if verbosity < 0 || verbosity > 4 {
		return nil, fmt.Errorf("verbosity must be 0-4, got %d", verbosity)
	}

	// Create logger with multi-output (console, WebSocket, file)
	serverLogger, wsCore, wsTransport, err := createGraphLogger(verbosity)
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}
	// Defensive: Verify logger components
	if serverLogger == nil || wsCore == nil || wsTransport == nil {
		return nil, fmt.Errorf("logger creation returned nil components")
	}

	// Create all server dependencies (builder, services, trackers, daemon)
	deps, err := createServerDependencies(db, verbosity, wsCore, wsTransport, serverLogger)
	if err != nil {
		return nil, fmt.Errorf("failed to create server dependencies: %w", err)
	}

	// Defensive: Validate critical dependencies (nil daemon is allowed)
	if deps.builder == nil {
		return nil, fmt.Errorf("graph builder creation failed")
	}
	// TODO(#54,#56): Validate langService and usageTracker when extracted
	// if deps.langService == nil {
	// 	return nil, fmt.Errorf("language service creation failed")
	// }
	// if deps.usageTracker == nil {
	// 	return nil, fmt.Errorf("usage tracker creation failed")
	// }
	if deps.budgetTracker == nil {
		return nil, fmt.Errorf("budget tracker creation failed")
	}

	// Create cancellation context for lifecycle management
	ctx, cancel := context.WithCancel(context.Background())

	// GRACE Phase 2: Recreate daemon with server's context for proper shutdown coordination
	// Reuse config from deps to avoid double-loading (optimization for WS connection speed)
	poolConfig := async.DefaultWorkerPoolConfig()
	if deps.config.Pulse.Workers > 0 {
		poolConfig.Workers = deps.config.Pulse.Workers
	}
	daemon := async.NewWorkerPoolWithContext(ctx, db, deps.config, poolConfig, serverLogger)

	// Create Pulse ticker for scheduled job execution
	scheduleStore := schedule.NewStore(db)
	tickerCfg := schedule.DefaultTickerConfig()
	// Override default with config if provided
	if deps.config.Pulse.TickerIntervalSeconds > 0 {
		tickerCfg.Interval = time.Duration(deps.config.Pulse.TickerIntervalSeconds) * time.Second
	}

	// Create server instance (before ticker so we can pass it as broadcaster)
	server := &QNTXServer{
		db:      db,
		dbPath:  dbPath,
		builder: deps.builder,
		// langService:   deps.langService,  // TODO(#54): Extract ats/lsp
		// usageTracker:  deps.usageTracker, // TODO(#56): Extract ai/tracker
		budgetTracker: deps.budgetTracker,
		daemon:        daemon, // Use daemon with server context
		ticker:        nil,    // Will be set below after passing server as broadcaster
		clients:       make(map[*Client]bool),
		broadcast:     make(chan *graph.Graph, MaxClientMessageQueueSize),
		register:      make(chan *Client),
		unregister:    make(chan *Client),
		logger:        serverLogger,
		logTransport:  wsTransport,
		wsCore:        wsCore,
		initialQuery:  initialQuery,
		ctx:           ctx,
		cancel:        cancel,
	}
	server.verbosity.Store(int32(verbosity))
	server.graphLimit.Store(1000) // Default graph node limit
	server.state.Store(int32(ServerStateRunning)) // GRACE Phase 4: Initialize to running

	// Create ticker with server as broadcaster for real-time execution updates
	ticker := schedule.NewTickerWithContext(ctx, scheduleStore, daemon.GetQueue(), daemon, server, tickerCfg, serverLogger)
	server.ticker = ticker

	// Set up config file watcher for auto-reload
	setupConfigWatcher(server, db, serverLogger)

	return server, nil
}

// createGraphLogger creates a multi-output zap logger (console + WebSocket + file)
func createGraphLogger(verbosity int) (*zap.SugaredLogger, *wslogs.WebSocketCore, *wslogs.Transport, error) {
	// Create WebSocket log transport
	wsTransport := wslogs.NewTransport()

	// Create WebSocket core for zap
	wsCore := wslogs.NewWebSocketCore(logger.VerbosityToLevel(verbosity))

	// Build multi-core logger: console + WebSocket + file (if verbosity >= 2)
	cores := []zapcore.Core{
		logger.Logger.Desugar().Core(), // Existing console/file core
		wsCore,                         // WebSocket core for UI
	}

	// Add file logging for verbosity >= 2
	if verbosity >= 2 {
		fileCore, err := createFileCore("tmp/graph-debug.log", verbosity)
		if err == nil {
			cores = append(cores, fileCore)
		}
		// Don't fail if file creation fails, just skip file logging
	}

	// Create tee core (multi-output)
	core := zapcore.NewTee(cores...)
	serverLogger := zap.New(core).Sugar().Named("server")

	return serverLogger, wsCore, wsTransport, nil
}

// createServerDependencies creates all dependencies needed by QNTXServer
func createServerDependencies(db *sql.DB, verbosity int, wsCore *wslogs.WebSocketCore, wsTransport *wslogs.Transport, serverLogger *zap.SugaredLogger) (*serverDependencies, error) {
	start := time.Now()

	// Create builder with server logger
	builder, err := graph.NewAxGraphBuilder(db, verbosity, serverLogger)
	if err != nil {
		return nil, fmt.Errorf("failed to create graph builder: %w", err)
	}
	serverLogger.Debugw("Graph builder created", "duration_ms", time.Since(start).Milliseconds())

	// TODO(#54): Extract ats/lsp - language service for LSP-like features deferred
	// langStart := time.Now()
	// symbolIndex, err := storage.NewSymbolIndex(db)
	// if err != nil {
	// 	return nil, fmt.Errorf("failed to create symbol index: %w", err)
	// }
	// langService := lsp.NewService(symbolIndex)
	// serverLogger.Debugw("Language service created", "duration_ms", time.Since(langStart).Milliseconds())

	// TODO(#56): Extract ai/tracker - usage tracker deferred
	// usageTracker := tracker.NewUsageTracker(db, verbosity)

	// Load configuration for daemon setup
	cfgStart := time.Now()
	cfg, err := appcfg.Load()
	if err != nil {
		// Log warning but continue with defaults
		serverLogger.Warnw("Failed to load config, using defaults for daemon", "error", err)
		cfg = &appcfg.Config{} // Will use default values
	}
	serverLogger.Debugw("Config loaded", "duration_ms", time.Since(cfgStart).Milliseconds())

	// Create budget tracker for Pulse daemon monitoring
	budgetTracker := budget.NewTracker(db, budget.BudgetConfig{
		DailyBudgetUSD:   cfg.Pulse.DailyBudgetUSD,
		WeeklyBudgetUSD:  cfg.Pulse.WeeklyBudgetUSD,
		MonthlyBudgetUSD: cfg.Pulse.MonthlyBudgetUSD,
		CostPerScoreUSD:  cfg.Pulse.CostPerScoreUSD,
	})

	// Create daemon (background job processor)
	// Note: Daemon gets its own context initially, but will be recreated with server context when server starts
	daemonStart := time.Now()
	daemon := async.NewWorkerPool(db, cfg, async.DefaultWorkerPoolConfig(), serverLogger)
	serverLogger.Debugw("Daemon created", "duration_ms", time.Since(daemonStart).Milliseconds())

	serverLogger.Debugw("All dependencies created", "total_duration_ms", time.Since(start).Milliseconds())

	return &serverDependencies{
		builder:       builder,
		// langService:   langService,  // TODO(#54): Extract ats/lsp
		// usageTracker:  usageTracker, // TODO(#56): Extract ai/tracker
		budgetTracker: budgetTracker,
		daemon:        daemon,
		config:        cfg, // GRACE Phase 2 optimization: save for reuse
	}, nil
}

// TODO(#57): Extract config watcher - auto-reload functionality deferred to future PR
// setupConfigWatcher sets up config file watching with reload callbacks
func setupConfigWatcher(server *QNTXServer, db *sql.DB, serverLogger *zap.SugaredLogger) {
	// Config watching disabled - requires extraction of appcfg.ConfigWatcher
	serverLogger.Debugw("Config watching disabled, manual restart required for config changes")
	return

	// // Watch the UI config file (where graph server config updates are written)
	// configPath := appcfg.GetUIConfigPath()
	// if configPath == "" {
	// 	serverLogger.Warnw("Could not determine UI config path, config watching disabled")
	// 	return
	// }
	//
	// configWatcher, err := appcfg.NewConfigWatcher(configPath)
	// if err != nil {
	// 	serverLogger.Warnw("Failed to create config watcher, manual restart required for config changes", "error", err)
	// 	return
	// }
	//
	// server.configWatcher = configWatcher
	//
	// // Set global watcher for persist.go to prevent reload loops
	// appcfg.SetGlobalWatcher(configWatcher)
	//
	// // Register callback to update BudgetTracker when config changes
	// configWatcher.OnReload(func(newCfg *appcfg.Config) error {
	// 	serverLogger.Infow("Config reloaded, updating budget tracker",
	// 		"daily_budget", newCfg.Pulse.DailyBudgetUSD,
	// 		"monthly_budget", newCfg.Pulse.MonthlyBudgetUSD,
	// 	)
	//
	// 	// Update budget tracker with new limits
	// 	server.budgetTracker = budget.NewTracker(db, budget.BudgetConfig{
	// 		DailyBudgetUSD:   newCfg.Pulse.DailyBudgetUSD,
	// 		MonthlyBudgetUSD: newCfg.Pulse.MonthlyBudgetUSD,
	// 		CostPerScoreUSD:  newCfg.Pulse.CostPerScoreUSD,
	// 	})
	//
	// 	// Broadcast updated daemon status to all clients (includes new budget limits)
	// 	server.broadcastDaemonStatus()
	//
	// 	return nil
	// })
	//
	// // Start watching for changes
	// configWatcher.Start()
	// serverLogger.Infow("Config watcher started", "path", configPath)
}
