package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/teranos/QNTX/ai/tracker"
	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/signing"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/glyph/handlers"
	glyphstorage "github.com/teranos/QNTX/glyph/storage"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/plugin"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/budget"
	"github.com/teranos/QNTX/pulse/schedule"
	"github.com/teranos/QNTX/server/auth"
	serverembeddings "github.com/teranos/QNTX/server/embeddings"
	"github.com/teranos/QNTX/server/nodedid"
	"go.uber.org/zap"
)

// serverDependencies holds dependencies created for QNTXServer
// Refactored: Adding a new dependency requires changes in 3-4 places (down from 6):
// 1. QNTXServer struct (server.go)
// 2. This struct
// 3. createServerDependencies() - create and add to return
// 4. (Optional) Global storage if needed (e.g., SetDefaultPluginManager)
type serverDependencies struct {
	usageTracker  *tracker.UsageTracker
	budgetTracker *budget.Tracker
	daemon        *async.WorkerPool
	pluginManager *grpcplugin.PluginManager
	config        *appcfg.Config
}

// NewQNTXServer creates a new QNTX server.
// atsStore is the pre-created attestation store (shared with the Rust SQL driver).
func NewQNTXServer(db *sql.DB, atsStore ats.AttestationStore, dbPath string, verbosity int) (*QNTXServer, error) {
	// Defensive: Validate critical inputs
	if db == nil {
		return nil, errors.New("database connection cannot be nil")
	}
	if atsStore == nil {
		return nil, errors.New("attestation store cannot be nil")
	}
	if verbosity < 0 || verbosity > 4 {
		return nil, errors.Newf("verbosity must be 0-4, got %d", verbosity)
	}

	// Resolve log path from config (includes port in default name)
	cfg, err := appcfg.Load()
	if err != nil {
		cfg = &appcfg.Config{}
	}
	logPath := cfg.GetLogPath(appcfg.GetServerPort())

	// Create server logger (wraps global logger with "server" namespace)
	serverLogger := createServerLogger(verbosity)

	// Create all server dependencies (builder, services, trackers, daemon)
	deps, err := createServerDependencies(db, atsStore, verbosity, serverLogger)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create server dependencies")
	}

	// Defensive: Validate critical dependencies (nil daemon is allowed)
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
	if deps.config.Pulse.Workers == 0 {
		// 0 = no background workers (disable worker pool)
		poolConfig.Workers = 0
	} else if deps.config.Pulse.Workers > 0 {
		// Use configured worker count (defaults to 1 if omitted from config file)
		poolConfig.Workers = deps.config.Pulse.Workers
	}

	// Create handler registry (empty - handlers will be registered asynchronously)
	// Plugin handlers are registered in cmd/qntx/main.go after plugins finish loading
	registry := async.NewHandlerRegistry()

	daemon := async.NewWorkerPoolWithRegistry(ctx, db, deps.config, poolConfig, serverLogger, registry, nil, nil)

	// Create Pulse ticker for scheduled job execution
	scheduleStore := schedule.NewStore(db)
	tickerCfg := schedule.DefaultTickerConfig()
	if deps.config.Pulse.TickerIntervalSeconds == 0 {
		// 0 = no periodic ticking (disable ticker)
		tickerCfg.Interval = 0
	} else if deps.config.Pulse.TickerIntervalSeconds > 0 {
		// Use configured interval (defaults to 1 second if omitted from config file)
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

	// Security: non-loopback bind requires authentication
	bindAddr := deps.config.Server.BindAddress
	if bindAddr == "" {
		bindAddr = "127.0.0.1"
	}
	if !appcfg.IsLoopbackAddress(bindAddr) && !deps.config.Auth.Enabled {
		cancel()
		return nil, errors.Newf(
			"auth.enabled must be true when server.bind_address is %q (non-loopback bind exposes all endpoints to the network)",
			bindAddr,
		)
	}

	// Initialize per-IP rate limiters from config
	rl := deps.config.Server.RateLimit

	// Create server instance (before ticker so we can pass it as broadcaster)
	server := &QNTXServer{
		db:            db,
		dbPath:        dbPath,
		logPath:       logPath,
		bindAddress:   bindAddr,
		usageTracker:  deps.usageTracker,
		budgetTracker: deps.budgetTracker,
		daemon:        daemon,             // Use daemon with server context
		pluginManager: deps.pluginManager, // May be nil if no plugins enabled
		ticker:        nil,                // Will be set below after passing server as broadcaster
		clients:       make(map[*Client]bool),
		broadcastReq:  make(chan *broadcastRequest, MaxClientMessageQueueSize*2), // 2x buffer for multiple message types
		register:      make(chan *Client),
		unregister:    make(chan *Client),
		logger:        serverLogger,
		consoleBuffer: consoleBuffer, // Browser console log buffer with terminal printing
		rlAuth:        newRateLimitGroup(rl.AuthRate, rl.AuthBurst),
		rlWS:          newRateLimitGroup(rl.WSRate, rl.WSBurst),
		rlWrite:       newRateLimitGroup(rl.WriteRate, rl.WriteBurst),
		rlRead:        newRateLimitGroup(rl.ReadRate, rl.ReadBurst),
		rlPublic:      newRateLimitGroup(rl.PublicRate, rl.PublicBurst),
		ctx:           ctx,
		cancel:        cancel,
	}
	server.verbosity.Store(int32(verbosity))
	server.state.Store(int32(ServerStateRunning)) // Opening/Closing Phase 4: Initialize to running

	// Set as global default server for async plugin initialization
	SetDefaultServer(server)

	// Initialize WebAuthn auth gate (if enabled in config)
	if deps.config.Auth.Enabled {
		serverPort := appcfg.DefaultServerPort
		if deps.config.Server.Port != nil {
			serverPort = *deps.config.Server.Port
		}
		// Auth routes: rate limit BEFORE CORS so brute-force attempts are rejected early.
		// CORS still runs first for OPTIONS preflight (corsMiddleware short-circuits OPTIONS with 200).
		authCorsWrap := func(handler http.HandlerFunc) http.HandlerFunc {
			return server.rateLimitAuthMiddleware(server.corsMiddleware(handler))
		}
		authHandler, err := auth.New(db, serverPort, deps.config.Server.FrontendPort, deps.config.Auth.SessionExpiryHours, serverLogger, authCorsWrap)
		if err != nil {
			return nil, errors.Wrap(err, "failed to initialize WebAuthn auth")
		}
		server.authHandler = authHandler
		server.authEnabled = true
		serverLogger.Infow("WebAuthn authentication enabled",
			"session_expiry_hours", deps.config.Auth.SessionExpiryHours,
		)
	}

	// Initialize node DID (decentralized identity for this node)
	nodeDIDHandler, err := nodedid.New(db, serverLogger)
	if err != nil {
		return nil, errors.Wrap(err, "failed to initialize node DID")
	}
	server.nodeDID = nodeDIDHandler

	// Set global signer so all attestations are signed with the node's DID key
	storage.SetDefaultSigner(signing.NewSigner(nodeDIDHandler.PrivateKey, nodeDIDHandler.DID))

	server.atsStore = atsStore

	// Register system type definitions so attestations render in the graph
	if err := types.EnsureTypes(atsStore, "prompt-direct", types.PromptResult); err != nil {
		serverLogger.Warnw("Failed to register prompt-result type", "error", err)
	}
	if err := types.EnsureTypes(atsStore, "cluster-labeling", types.ClusterLabeled); err != nil {
		serverLogger.Warnw("Failed to register cluster-labeled type", "error", err)
	}

	// Initialize domain plugin registry
	pluginRegistry := plugin.GetDefaultRegistry()
	if pluginRegistry != nil {
		server.pluginRegistry = pluginRegistry

		queue := daemon.GetQueue()

		// Start gRPC services for plugins (Issue #138)
		// These services allow plugins to call back to QNTX core
		servicesManager := grpcplugin.NewServicesManager(deps.config.LLM, serverLogger)
		filesDir := filepath.Join(filepath.Dir(dbPath), "files")

		endpoints, err := servicesManager.Start(ctx, atsStore, queue, scheduleStore, filesDir, deps.config.GroundDBPath)
		if err != nil {
			serverLogger.Warnw("Failed to start plugin services, plugins will not have service access", "error", err)
			endpoints = nil
		} else {
			serverLogger.Debugw("Plugin services started",
				"ats_store", endpoints.ATSStoreAddress,
				"queue", endpoints.QueueAddress,
				"schedule", endpoints.ScheduleAddress,
				"file_service", endpoints.FileServiceAddress,
				"llm", endpoints.LLMAddress,
				"embedding", endpoints.EmbeddingAddress,
				"search", endpoints.SearchAddress,
			)
		}

		// Wrap config provider to inject service endpoints for plugins
		configProvider := grpcplugin.NewConfigProvider(endpoints)

		services := plugin.NewServiceRegistry(db, serverLogger, atsStore, configProvider, queue)

		// Store services manager and registry for shutdown and reinitialization
		server.servicesManager = servicesManager
		server.services = services

		// Wire services manager to plugin manager for LLM provider re-registration after restart.
		// Note: server.pluginManager is often nil here because plugins load asynchronously.
		// getPluginManager() handles the lazy wiring for the global default plugin manager.
		if server.pluginManager != nil {
			server.pluginManager.SetServicesManager(servicesManager)
		}
	}

	// Initialize gRPC plugins (if any are loaded)
	// IMPORTANT: This must happen during server startup, not lazily on first HTTP request.
	serverLogger.Debugw("Plugin manager check", "plugin_manager_is_nil", server.pluginManager == nil, "services_is_nil", server.services == nil)

	// Log plugin registry state — plugins load asynchronously, so the manager
	// is typically nil here. This captures the registry state in the structured
	// log file (plugin-loader only writes to terminal).
	if pluginRegistry != nil {
		states := pluginRegistry.GetAllStates()
		for name, state := range states {
			errMsg, _ := pluginRegistry.GetError(name)
			serverLogger.Debugw("Plugin state at server startup",
				"plugin", name, "state", state, "error", errMsg)
		}
	}

	// Create ticker with server as broadcaster for real-time execution updates
	ticker := schedule.NewTickerWithContext(ctx, scheduleStore, daemon.GetQueue(), daemon, server, tickerCfg, serverLogger)
	server.ticker = ticker

	// Create and start storage events poller for broadcasting warnings/evictions
	storagePoller := NewStorageEventsPoller(db, server, serverLogger)
	server.storageEventsPoller = storagePoller
	ticker.SetEvictionStats(storagePoller)

	// Track attestation creation counts for periodic summary logging
	creationStats := NewCreationStatsObserver()
	storage.RegisterObserver(creationStats)
	ticker.SetCreationStats(creationStats)

	// Index attestations into MeiliSearch when a search provider is available (ADR-015).
	// The observer checks HasProvider() on each write — no-op when meili isn't running.
	if server.servicesManager != nil {
		richStore := storage.NewBoundedStore(db, nil, serverLogger.Named("search-index"))
		searchObserver := NewSearchIndexObserver(server.servicesManager, richStore, serverLogger.Named("search-index"))
		storage.RegisterObserver(searchObserver)
	}

	// Configure periodic database backup via Rust's hot backup API
	backupInterval := time.Duration(deps.config.Database.BackupIntervalSeconds) * time.Second
	if bp, ok := atsStore.(schedule.BackupProvider); ok && backupInterval > 0 {
		ticker.SetBackupProvider(bp, dbPath, backupInterval)
		// TODO: make backup retention count configurable
	}
	server.wg.Add(1)
	go func() {
		defer server.wg.Done()
		storagePoller.Start(ctx)
	}()

	// Initialize watcher engine for reactive attestation triggers
	if err := server.initWatcherEngine(); err != nil {
		serverLogger.Warnw("Failed to initialize watcher engine", "error", err)
		// Non-fatal: server can still run without watchers
	}

	// Initialize canvas state handlers — with watcher engine for meld edge subscriptions
	canvasStore := glyphstorage.NewCanvasStore(db)
	var canvasOpts []handlers.CanvasHandlerOption
	if server.watcherEngine != nil {
		canvasOpts = append(canvasOpts, handlers.WithWatcherEngine(server.watcherEngine, serverLogger))
	}
	// Pass server port for internal plugin calls
	serverPort := appcfg.DefaultServerPort
	if deps.config.Server.Port != nil {
		serverPort = *deps.config.Server.Port
	}
	canvasOpts = append(canvasOpts, handlers.WithServerPort(serverPort))
	server.canvasHandler = handlers.NewCanvasHandler(canvasStore, canvasOpts...)
	server.conversationAssembler = NewConversationAssembler(canvasStore, storage.NewSQLQueryStore(db))
	serverLogger.Debugw("Canvas state handlers initialized")

	// Initialize embedding service for semantic search (optional)
	server.groundDBPath = deps.config.GroundDBPath
	server.SetupEmbeddingService()

	// Use the primary rustsqlite connection for reads — the Rust driver
	// separates reads/writes internally (muRead/muWrite), no need for a
	// separate mattn read-only connection (which caused SIGBUS crashes from
	// concurrent CGO calls via different drivers on the same file).
	server.embeddingsHandler = &serverembeddings.Handler{
		DB:           db,
		ReadDB:       db,
		Store:        server.embeddingStore,
		Service:      server.embeddingService,
		ATSStore:     atsStore,
		Logger:       serverLogger,
		CallReduce:   server.callReducePlugin,
		Invalidator:  server.embeddingClusterInvalidator,
		GroundDBPath: deps.config.GroundDBPath,
		GroundWrite:  writeToGround,
	}
	if server.embeddingStats != nil {
		ticker.SetEmbeddingStats(server.embeddingStats)
	}
	if server.servicesManager != nil {
		if llmRouter := server.servicesManager.GetLLMRouter(); llmRouter != nil {
			ticker.SetWeaveStats(llmRouter)
		}
	}
	server.setupDistillSchedule(deps.config)
	server.setupCheckpointSchedule(deps.config)
	server.setupEmbeddingReclusterSchedule(deps.config)
	server.setupEmbeddingReprojectSchedule(deps.config)
	server.setupClusterLabelSchedule(deps.config)

	// Wire embedding service into gRPC for plugin access
	if server.embeddingService != nil && server.servicesManager != nil {
		if router := server.servicesManager.GetEmbeddingRouter(); router != nil {
			router.SetService(server.embeddingService)
		}
	}
	if server.embeddingStore != nil && server.servicesManager != nil {
		if router := server.servicesManager.GetEmbeddingRouter(); router != nil {
			router.SetStore(server.embeddingStore)
		}
	}

	// Wire embedding service into watcher engine now that it's available
	// (watcher engine starts before embeddings — reconnect and reload)
	if server.embeddingService != nil && server.watcherEngine != nil {
		server.watcherEngine.SetEmbeddingService(&watcherEmbeddingAdapter{svc: server.embeddingService})
		if server.embeddingStore != nil {
			server.watcherEngine.SetEmbeddingSearcher(&watcherSearchAdapter{store: server.embeddingStore})
		}
		if err := server.watcherEngine.ReloadWatchers(); err != nil {
			serverLogger.Warnw("Failed to reload watchers after embedding service init", "error", err)
		}
	}

	// Set up config file watcher for auto-reload
	setupConfigWatcher(server, db, serverLogger)

	return server, nil
}

// createServerLogger creates a named server logger from the global logger.
func createServerLogger(verbosity int) *zap.SugaredLogger {
	return logger.Logger.Desugar().Named("server").Sugar()
}

// createServerDependencies creates all components needed for QNTXServer initialization
func createServerDependencies(db *sql.DB, atsStore ats.AttestationStore, verbosity int, serverLogger *zap.SugaredLogger) (*serverDependencies, error) {
	start := time.Now()

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
		DailyBudgetUSD:          cfg.Pulse.DailyBudgetUSD,
		WeeklyBudgetUSD:         cfg.Pulse.WeeklyBudgetUSD,
		MonthlyBudgetUSD:        cfg.Pulse.MonthlyBudgetUSD,
		CostPerScoreUSD:         cfg.Pulse.CostPerScoreUSD,
		ClusterDailyBudgetUSD:   cfg.Pulse.ClusterDailyBudgetUSD,
		ClusterWeeklyBudgetUSD:  cfg.Pulse.ClusterWeeklyBudgetUSD,
		ClusterMonthlyBudgetUSD: cfg.Pulse.ClusterMonthlyBudgetUSD,
	})

	// Create daemon (background job processor)
	// Note: Daemon gets its own context initially, but will be recreated with server context when server starts
	daemonStart := time.Now()
	daemon := async.NewWorkerPool(db, cfg, async.DefaultWorkerPoolConfig(), serverLogger)
	serverLogger.Debugw("Daemon created", "duration_ms", time.Since(daemonStart).Milliseconds())

	// Retrieve plugin manager from global storage (set by main.go during plugin initialization)
	pluginManager := grpcplugin.GetDefaultPluginManager()

	serverLogger.Debugw("All dependencies created", "total_duration_ms", time.Since(start).Milliseconds())

	return &serverDependencies{
		usageTracker:  usageTracker,
		budgetTracker: budgetTracker,
		daemon:        daemon,
		pluginManager: pluginManager, // May be nil if no plugins are enabled
		config:        cfg,           // GRACE Phase 2 optimization: save for reuse
	}, nil
}

// setupConfigWatcher sets up config file watching with reload callbacks
func setupConfigWatcher(server *QNTXServer, db *sql.DB, serverLogger *zap.SugaredLogger) {
	// Get the project config file path
	configPath := appcfg.ProjectConfigPath()
	if configPath == "" {
		serverLogger.Debugw("No config file found, using defaults (config watching disabled)")
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
			DailyBudgetUSD:          newCfg.Pulse.DailyBudgetUSD,
			WeeklyBudgetUSD:         newCfg.Pulse.WeeklyBudgetUSD,
			MonthlyBudgetUSD:        newCfg.Pulse.MonthlyBudgetUSD,
			CostPerScoreUSD:         newCfg.Pulse.CostPerScoreUSD,
			ClusterDailyBudgetUSD:   newCfg.Pulse.ClusterDailyBudgetUSD,
			ClusterWeeklyBudgetUSD:  newCfg.Pulse.ClusterWeeklyBudgetUSD,
			ClusterMonthlyBudgetUSD: newCfg.Pulse.ClusterMonthlyBudgetUSD,
		})

		// Broadcast updated daemon status to all clients (includes new budget limits)
		server.broadcastDaemonStatus()

		return nil
	})

	// Register callback to hot-swap plugins when [plugin].enabled changes
	configWatcher.OnReload(func(newCfg *appcfg.Config) error {
		manager := grpcplugin.GetDefaultPluginManager()
		registry := plugin.GetDefaultRegistry()
		if manager == nil || registry == nil {
			serverLogger.Warnw("Plugin hot-swap skipped: manager or registry not initialized",
				"manager_nil", manager == nil, "registry_nil", registry == nil)
			return nil
		}

		// Build sets for diff
		nowEnabled := make(map[string]bool, len(newCfg.Plugin.Enabled))
		for _, name := range newCfg.Plugin.Enabled {
			nowEnabled[name] = true
		}
		currentlyLoaded := make(map[string]bool)
		for _, name := range manager.LoadedPluginNames() {
			currentlyLoaded[name] = true
		}

		// Disable plugins that were removed from enabled list
		for name := range currentlyLoaded {
			if !nowEnabled[name] {
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				if err := manager.DisablePlugin(ctx, name, registry); err != nil {
					serverLogger.Errorw("Failed to disable plugin", "plugin", name, "error", err)
				}
				cancel()
				server.InvalidatePluginMux(name)
				server.broadcastDaemonStatus()
			}
		}

		// Enable plugins that were added to enabled list
		for name := range nowEnabled {
			if !currentlyLoaded[name] {
				go func(pluginName string) {
					defer func() {
						if r := recover(); r != nil {
							serverLogger.Errorw("Plugin enable panicked", "plugin", pluginName, "panic", r)
						}
					}()
					ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
					defer cancel()
					services := server.GetServices()
					if err := manager.EnablePlugin(ctx, pluginName, newCfg.Plugin.Paths, registry, services); err != nil {
						serverLogger.Errorw("Failed to enable plugin", "plugin", pluginName, "error", err)
					}
					server.broadcastDaemonStatus()
				}(name)
			}
		}

		return nil
	})

	// Start watching for changes
	configWatcher.Start()
	serverLogger.Infow("Config watcher started", "path", configPath)
}

