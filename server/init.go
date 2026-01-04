package server

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/teranos/QNTX/ai/tracker"
	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/lsp"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/qntx-code/langserver/gopls"
	"github.com/teranos/QNTX/graph"
	"github.com/teranos/QNTX/logger"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/budget"
	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/QNTX/server/wslogs"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// serverDependencies holds all dependencies created for QNTXServer
type serverDependencies struct {
	builder       *graph.AxGraphBuilder
	langService   *lsp.Service   // Language service for ATS LSP features
	goplsService  *gopls.Service // Language service for Go code intelligence
	usageTracker  *tracker.UsageTracker
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
	if deps.langService == nil {
		return nil, fmt.Errorf("language service creation failed")
	}
	if deps.usageTracker == nil {
		return nil, fmt.Errorf("usage tracker creation failed")
	}
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

	// Create console buffer with callback to print logs to terminal
	consoleBuffer := NewConsoleBuffer(100)
	consoleBuffer.onNewLog = func(log ConsoleLog) {
		// Format log level for display
		levelIcon := map[string]string{
			"error": "✗",
			"warn":  "⚠",
			"info":  "ℹ",
			"debug": "→",
		}
		icon := levelIcon[log.Level]
		if icon == "" {
			icon = "·"
		}

		// Print to server terminal
		serverLogger.Infow(fmt.Sprintf("[Browser %s] %s", icon, log.Message),
			"level", log.Level,
			"url", log.URL,
		)

		// Also print stack trace for errors
		if log.Level == "error" && log.Stack != "" {
			serverLogger.Debugw("Browser error stack trace",
				"stack", log.Stack,
			)
		}
	}

	// Create server instance (before ticker so we can pass it as broadcaster)
	server := &QNTXServer{
		db:            db,
		dbPath:        dbPath,
		builder:       deps.builder,
		langService:   deps.langService,
		goplsService:  deps.goplsService,
		usageTracker:  deps.usageTracker,
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
		consoleBuffer: consoleBuffer, // Browser console log buffer with terminal printing
		initialQuery:  initialQuery,
		ctx:           ctx,
		cancel:        cancel,
	}
	server.verbosity.Store(int32(verbosity))
	server.graphLimit.Store(1000) // Default graph node limit
	server.state.Store(int32(ServerStateRunning)) // GRACE Phase 4: Initialize to running

	// Initialize domain plugin registry
	pluginRegistry := domains.GetDefaultRegistry()
	if pluginRegistry != nil {
		server.pluginRegistry = pluginRegistry

		// Initialize plugins with services
		store := storage.NewSQLStore(db, serverLogger)
		queue := daemon.GetQueue()
		services := domains.NewServiceRegistry(db, serverLogger, store, &simpleConfigProvider{}, queue)

		if err := pluginRegistry.InitializeAll(ctx, services); err != nil {
			serverLogger.Errorw("Failed to initialize domain plugins", "error", err)
			// Continue anyway - plugins are optional
		} else {
			serverLogger.Infow("Domain plugins initialized", "count", len(pluginRegistry.List()))
		}
	}

	// Create ticker with server as broadcaster for real-time execution updates
	ticker := schedule.NewTickerWithContext(ctx, scheduleStore, daemon.GetQueue(), daemon, server, tickerCfg, serverLogger)
	server.ticker = ticker

	// Create and start storage events poller for broadcasting warnings/evictions
	storagePoller := NewStorageEventsPoller(db, server, serverLogger)
	server.storageEventsPoller = storagePoller
	server.wg.Add(1)
	go func() {
		defer server.wg.Done()
		storagePoller.Start(ctx)
	}()

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

	// Create language service for ATS LSP features
	langStart := time.Now()
	symbolIndex, err := storage.NewSymbolIndex(db)
	if err != nil {
		return nil, fmt.Errorf("failed to create symbol index: %w", err)
	}
	langService := lsp.NewService(symbolIndex)
	serverLogger.Debugw("Language service created", "duration_ms", time.Since(langStart).Milliseconds())

	// Load configuration for gopls and daemon setup
	cfgStart := time.Now()
	cfg, err := appcfg.Load()
	if err != nil {
		// Log warning but continue with defaults
		serverLogger.Warnw("Failed to load config, using defaults", "error", err)
		cfg = &appcfg.Config{} // Will use default values
	}
	serverLogger.Debugw("Config loaded", "duration_ms", time.Since(cfgStart).Milliseconds())

	// Create gopls service for Go code intelligence (if enabled)
	var goplsService *gopls.Service
	if cfg.Code.Gopls.Enabled {
		workspaceRoot := cfg.Code.Gopls.WorkspaceRoot
		if workspaceRoot == "" || workspaceRoot == "." {
			// Convert to absolute path
			if absPath, err := filepath.Abs("."); err == nil {
				workspaceRoot = absPath
			} else {
				workspaceRoot = "."
			}
		}
		goplsService, err = gopls.NewService(gopls.Config{
			WorkspaceRoot: workspaceRoot,
			Logger:        serverLogger,
		})
		if err != nil {
			serverLogger.Warnw("Failed to create gopls service, Go code intelligence disabled", "error", err)
			goplsService = nil
		} else {
			// Initialize gopls service
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := goplsService.Initialize(ctx); err != nil {
				serverLogger.Warnw("Failed to initialize gopls, Go code intelligence disabled", "error", err)
				goplsService = nil
			} else {
				serverLogger.Infow(fmt.Sprintf("gopls service initialized (workspace: %s)", workspaceRoot))
			}
		}
	} else {
		serverLogger.Debugw("gopls service disabled in config")
	}

	// Create usage tracker for AI model usage and cost monitoring
	usageTracker := tracker.NewUsageTracker(db, verbosity)

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
		langService:   langService,
		goplsService:  goplsService,
		usageTracker:  usageTracker,
		budgetTracker: budgetTracker,
		daemon:        daemon,
		config:        cfg, // GRACE Phase 2 optimization: save for reuse
	}, nil
}

// setupConfigWatcher sets up config file watching with reload callbacks
func setupConfigWatcher(server *QNTXServer, db *sql.DB, serverLogger *zap.SugaredLogger) {
	// Get the config file path from Viper
	configPath := appcfg.GetViper().ConfigFileUsed()
	if configPath == "" {
		serverLogger.Infow("No config file found, using defaults (config watching disabled)")
		return
	}

	serverLogger.Infow(fmt.Sprintf("Using config file: %s", configPath))

	configWatcher, err := appcfg.NewConfigWatcher(configPath)
	if err != nil {
		serverLogger.Warnw("Failed to create config watcher, manual restart required for config changes", "error", err)
		return
	}

	server.configWatcher = configWatcher

	// Set global watcher to prevent reload loops
	appcfg.SetGlobalWatcher(configWatcher)

	// Register callback to update BudgetTracker when config changes
	configWatcher.OnReload(func(newCfg *appcfg.Config) error {
		serverLogger.Infow("Config reloaded, updating budget tracker",
			"daily_budget", newCfg.Pulse.DailyBudgetUSD,
			"weekly_budget", newCfg.Pulse.WeeklyBudgetUSD,
			"monthly_budget", newCfg.Pulse.MonthlyBudgetUSD,
		)

		// Update budget tracker with new limits
		server.budgetTracker = budget.NewTracker(db, budget.BudgetConfig{
			DailyBudgetUSD:   newCfg.Pulse.DailyBudgetUSD,
			WeeklyBudgetUSD:  newCfg.Pulse.WeeklyBudgetUSD,
			MonthlyBudgetUSD: newCfg.Pulse.MonthlyBudgetUSD,
			CostPerScoreUSD:  newCfg.Pulse.CostPerScoreUSD,
		})

		// Broadcast updated daemon status to all clients (includes new budget limits)
		server.broadcastDaemonStatus()

		return nil
	})

	// Start watching for changes
	configWatcher.Start()
	serverLogger.Infow("Config watcher started", "path", configPath)
}

// simpleConfigProvider provides plugin configuration
type simpleConfigProvider struct{}

func (p *simpleConfigProvider) GetPluginConfig(domain string) domains.Config {
	return &simpleConfig{domain: domain}
}

// simpleConfig implements domains.Config using am package
type simpleConfig struct {
	domain string
}

func (c *simpleConfig) GetString(key string) string {
	return appcfg.GetString(c.domain + "." + key)
}

func (c *simpleConfig) GetInt(key string) int {
	return appcfg.GetInt(c.domain + "." + key)
}

func (c *simpleConfig) GetBool(key string) bool {
	return appcfg.GetBool(c.domain + "." + key)
}

func (c *simpleConfig) GetStringSlice(key string) []string {
	return appcfg.GetStringSlice(c.domain + "." + key)
}

func (c *simpleConfig) Get(key string) interface{} {
	return appcfg.Get(c.domain + "." + key)
}

func (c *simpleConfig) Set(key string, value interface{}) {
	appcfg.Set(c.domain+"."+key, value)
}
