package server

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/teranos/QNTX/ai/tracker"
	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats/lsp"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/auth"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/graph"
	"github.com/teranos/QNTX/logger"
	"github.com/teranos/QNTX/plugin"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
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
	langService   *lsp.Service // Language service for ATS LSP features
	usageTracker  *tracker.UsageTracker
	budgetTracker *budget.Tracker
	daemon        *async.WorkerPool
	config        *appcfg.Config // Opening/Closing Phase 2 optimization: reuse for daemon recreation
}

// NewQNTXServer creates a new QNTX server
func NewQNTXServer(db *sql.DB, dbPath string, verbosity int) (*QNTXServer, error) {
	return NewQNTXServerWithInitialQuery(db, dbPath, verbosity, "")
}

// NewQNTXServerWithInitialQuery creates a QNTXServer with an optional pre-loaded Ax query
func NewQNTXServerWithInitialQuery(db *sql.DB, dbPath string, verbosity int, initialQuery string) (*QNTXServer, error) {
	// Defensive: Validate critical inputs
	if db == nil {
		return nil, errors.New("database connection cannot be nil")
	}
	if verbosity < 0 || verbosity > 4 {
		return nil, errors.Newf("verbosity must be 0-4, got %d", verbosity)
	}

	// Create logger with multi-output (console, WebSocket, file)
	serverLogger, wsCore, wsTransport, err := createGraphLogger(verbosity)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create logger")
	}
	// Defensive: Verify logger components
	if serverLogger == nil || wsCore == nil || wsTransport == nil {
		return nil, errors.New("logger creation returned nil components")
	}

	// Create all server dependencies (builder, services, trackers, daemon)
	deps, err := createServerDependencies(db, verbosity, wsCore, wsTransport, serverLogger)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create server dependencies")
	}

	// Defensive: Validate critical dependencies (nil daemon is allowed)
	if deps.builder == nil {
		return nil, errors.New("graph builder creation failed")
	}
	if deps.langService == nil {
		return nil, errors.New("language service creation failed")
	}
	if deps.usageTracker == nil {
		return nil, errors.New("usage tracker creation failed")
	}
	if deps.budgetTracker == nil {
		return nil, errors.New("budget tracker creation failed")
	}

	// Create cancellation context for lifecycle management
	ctx, cancel := context.WithCancel(context.Background())

	// Opening/Closing Phase 2: Recreate daemon with server's context for proper shutdown coordination
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
	formatter := NewConsoleFormatter(verbosity) // Verbosity-aware formatting
	consoleBuffer.onNewLog = func(log ConsoleLog) {
		// Format message using custom formatter for JSON summarization and coloring
		formattedMsg := formatter.FormatMessage(log.Message)

		// Prefix with [Browser] to make it obvious where this log came from
		browserMsg := fmt.Sprintf("[Browser] %s", formattedMsg)

		// Log through zap to match existing log style
		// Use Infow for consistency with other logs (zap will add its own coloring)
		switch log.Level {
		case "error":
			serverLogger.Errorw(browserMsg,
				"url", log.URL,
			)
			// Also print stack trace for errors at debug level
			if log.Stack != "" {
				serverLogger.Debugw("Browser error stack trace",
					"stack", log.Stack,
				)
			}
		case "warn":
			serverLogger.Warnw(browserMsg,
				"url", log.URL,
			)
		case "debug":
			serverLogger.Debugw(browserMsg,
				"url", log.URL,
			)
		default: // info
			serverLogger.Infow(browserMsg,
				"url", log.URL,
			)
		}
	}

	// Create server instance (before ticker so we can pass it as broadcaster)
	server := &QNTXServer{
		db:            db,
		dbPath:        dbPath,
		builder:       deps.builder,
		langService:   deps.langService,
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
	server.graphLimit.Store(1000)                 // Default graph node limit
	server.state.Store(int32(ServerStateRunning)) // Opening/Closing Phase 4: Initialize to running

	// Initialize domain plugin registry
	pluginRegistry := plugin.GetDefaultRegistry()
	if pluginRegistry != nil {
		server.pluginRegistry = pluginRegistry

		// Initialize plugins with services
		store := storage.NewSQLStore(db, serverLogger)
		queue := daemon.GetQueue()

		// Start gRPC services for external plugins (Issue #138)
		// These services allow external plugins to call back to QNTX core
		servicesManager := grpcplugin.NewServicesManager(serverLogger)
		endpoints, err := servicesManager.Start(ctx, store, queue)
		if err != nil {
			serverLogger.Warnw("Failed to start plugin services, external plugins will not have service access", "error", err)
			endpoints = nil
		} else {
			serverLogger.Infow("Plugin services started",
				"ats_store", endpoints.ATSStoreAddress,
				"queue", endpoints.QueueAddress,
			)
		}

		// Wrap config provider to inject service endpoints for external plugins
		configProvider := &pluginConfigProvider{
			base:      &simpleConfigProvider{},
			endpoints: endpoints,
		}

		services := plugin.NewServiceRegistry(db, serverLogger, store, configProvider, queue)

		if err := pluginRegistry.InitializeAll(ctx, services); err != nil {
			serverLogger.Errorw("Failed to initialize domain plugins", "error", err)
			// Continue anyway - plugins are optional
		} else {
			serverLogger.Infow("Domain plugins initialized", "count", len(pluginRegistry.List()))
		}

		// Store services manager for shutdown
		server.servicesManager = servicesManager
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

	// Initialize auth service if enabled
	setupAuth(server, db, deps.config, serverLogger)

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
		return nil, errors.Wrap(err, "failed to create graph builder")
	}
	serverLogger.Debugw("Graph builder created", "duration_ms", time.Since(start).Milliseconds())

	// Create language service for ATS LSP features
	langStart := time.Now()
	symbolIndex, err := storage.NewSymbolIndex(db)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create symbol index")
	}
	langService := lsp.NewService(symbolIndex)
	serverLogger.Debugw("Language service created", "duration_ms", time.Since(langStart).Milliseconds())

	// Load configuration for daemon setup
	cfgStart := time.Now()
	cfg, err := appcfg.Load()
	if err != nil {
		// Log warning but continue with defaults
		serverLogger.Warnw("Failed to load config, using defaults", "error", err)
		cfg = &appcfg.Config{} // Will use default values
	}
	serverLogger.Debugw("Config loaded", "duration_ms", time.Since(cfgStart).Milliseconds())

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

func (p *simpleConfigProvider) GetPluginConfig(domain string) plugin.Config {
	return &simpleConfig{domain: domain}
}

// simpleConfig implements plugin.Config using am package
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

func (c *simpleConfig) GetKeys() []string {
	// Return empty list for now - could be enhanced to return actual keys from viper
	return []string{}
}

// pluginConfigProvider wraps a base config provider to inject service endpoints
type pluginConfigProvider struct {
	base      plugin.ConfigProvider
	endpoints *grpcplugin.ServiceEndpoints
}

func (p *pluginConfigProvider) GetPluginConfig(domain string) plugin.Config {
	baseConfig := p.base.GetPluginConfig(domain)
	return &pluginConfigWithEndpoints{
		base:      baseConfig,
		endpoints: p.endpoints,
	}
}

// pluginConfigWithEndpoints wraps a plugin config to inject service endpoints
type pluginConfigWithEndpoints struct {
	base      plugin.Config
	endpoints *grpcplugin.ServiceEndpoints
}

func (c *pluginConfigWithEndpoints) GetString(key string) string {
	// Inject service endpoints for external plugins (Issue #138)
	if c.endpoints != nil {
		switch key {
		case "_ats_store_endpoint":
			return c.endpoints.ATSStoreAddress
		case "_queue_endpoint":
			return c.endpoints.QueueAddress
		case "_auth_token":
			return c.endpoints.AuthToken
		}
	}
	return c.base.GetString(key)
}

func (c *pluginConfigWithEndpoints) GetInt(key string) int {
	return c.base.GetInt(key)
}

func (c *pluginConfigWithEndpoints) GetBool(key string) bool {
	return c.base.GetBool(key)
}

func (c *pluginConfigWithEndpoints) GetStringSlice(key string) []string {
	return c.base.GetStringSlice(key)
}

func (c *pluginConfigWithEndpoints) Get(key string) interface{} {
	// Inject service endpoints for external plugins
	if c.endpoints != nil {
		switch key {
		case "_ats_store_endpoint":
			return c.endpoints.ATSStoreAddress
		case "_queue_endpoint":
			return c.endpoints.QueueAddress
		case "_auth_token":
			return c.endpoints.AuthToken
		}
	}
	return c.base.Get(key)
}

func (c *pluginConfigWithEndpoints) Set(key string, value interface{}) {
	c.base.Set(key, value)
}

func (c *pluginConfigWithEndpoints) GetKeys() []string {
	return c.base.GetKeys()
}

// setupAuth initializes the authentication service if enabled
func setupAuth(server *QNTXServer, db *sql.DB, config *appcfg.Config, serverLogger *zap.SugaredLogger) {
	if !config.Auth.Enabled {
		serverLogger.Infow("Authentication disabled (local-only mode)")
		return
	}

	// Create auth store
	authStore := auth.NewStore(db)
	server.authStore = authStore

	// Create auth service
	authService, err := auth.NewService(db, &config.Auth, serverLogger)
	if err != nil {
		serverLogger.Warnw("Failed to initialize auth service, auth will be disabled", "error", err)
		return
	}

	server.authService = authService

	// Create middleware and handlers
	server.authMiddleware = auth.NewMiddleware(authService, authStore, serverLogger)
	server.authHandlers = auth.NewHandlers(authService, authStore, serverLogger)

	serverLogger.Infow("Authentication enabled",
		"providers", authService.ListProviders(),
		"tls_enabled", config.Auth.TLS.Enabled,
	)
}
