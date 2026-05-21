package server

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/teranos/QNTX/ai/tracker"
	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/errors"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/plugin"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/budget"
	"github.com/teranos/QNTX/pulse/schedule"
	"go.uber.org/zap"
)

// serverDependencies holds dependencies created for QNTXServer.
// Available to subsystems via s.deps during Init.
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
	if db == nil {
		return nil, errors.New("database connection cannot be nil")
	}
	if atsStore == nil {
		return nil, errors.New("attestation store cannot be nil")
	}
	if verbosity < 0 || verbosity > 4 {
		return nil, errors.Newf("verbosity must be 0-4, got %d", verbosity)
	}

	cfg, err := appcfg.Load()
	if err != nil {
		cfg = &appcfg.Config{}
	}
	logPath := cfg.GetLogPath(appcfg.GetServerPort())
	serverLogger := createServerLogger(verbosity)

	deps, err := createServerDependencies(db, cfg, serverLogger)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create server dependencies")
	}
	if deps.usageTracker == nil {
		return nil, errors.New("usage tracker creation failed")
	}
	if deps.budgetTracker == nil {
		return nil, errors.New("budget tracker creation failed")
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Worker pool with server's context for proper shutdown coordination
	poolConfig := async.DefaultWorkerPoolConfig()
	if deps.config.Pulse.Workers == 0 {
		poolConfig.Workers = 0
	} else if deps.config.Pulse.Workers > 0 {
		poolConfig.Workers = deps.config.Pulse.Workers
	}
	registry := async.NewHandlerRegistry()
	daemon := async.NewWorkerPoolWithRegistry(ctx, db, deps.config, poolConfig, serverLogger, registry, nil, nil)

	// Schedule store and ticker config (used by ticker subsystem)
	scheduleStore := schedule.NewStore(db)
	tickerCfg := schedule.DefaultTickerConfig()
	if deps.config.Pulse.TickerIntervalSeconds == 0 {
		tickerCfg.Interval = 0
	} else if deps.config.Pulse.TickerIntervalSeconds > 0 {
		tickerCfg.Interval = time.Duration(deps.config.Pulse.TickerIntervalSeconds) * time.Second
	}

	// Console buffer for browser log forwarding
	consoleBuffer := NewConsoleBuffer(100)
	formatter := NewConsoleFormatter(verbosity)
	consoleBuffer.onNewLog = func(log ConsoleLog) {
		formattedMsg := formatter.FormatMessage(log.Message)
		browserMsg := fmt.Sprintf("[Browser] %s", formattedMsg)
		switch log.Level {
		case "error":
			serverLogger.Errorw(browserMsg, "url", log.URL)
			if log.Stack != "" {
				serverLogger.Debugw("Browser error stack trace", "stack", log.Stack)
			}
		case "warn":
			serverLogger.Warnw(browserMsg, "url", log.URL)
		case "debug":
			serverLogger.Debugw(browserMsg, "url", log.URL)
		default:
			serverLogger.Infow(browserMsg, "url", log.URL)
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

	rl := deps.config.Server.RateLimit

	server := &QNTXServer{
		db:            db,
		dbPath:        dbPath,
		logPath:       logPath,
		deps:          deps,
		bindAddress:   bindAddr,
		usageTracker:  deps.usageTracker,
		budgetTracker: deps.budgetTracker,
		daemon:        daemon,
		pluginManager: deps.pluginManager,
		scheduleStore: scheduleStore,
		tickerCfg:     tickerCfg,
		clients:       make(map[*Client]bool),
		broadcastReq:  make(chan *broadcastRequest, MaxClientMessageQueueSize*2),
		register:      make(chan *Client),
		unregister:    make(chan *Client),
		logger:        serverLogger,
		consoleBuffer: consoleBuffer,
		rlAuth:        newRateLimitGroup(rl.AuthRate, rl.AuthBurst),
		rlWS:          newRateLimitGroup(rl.WSRate, rl.WSBurst),
		rlWrite:       newRateLimitGroup(rl.WriteRate, rl.WriteBurst),
		rlRead:        newRateLimitGroup(rl.ReadRate, rl.ReadBurst),
		rlPublic:      newRateLimitGroup(rl.PublicRate, rl.PublicBurst),
		ctx:           ctx,
		cancel:        cancel,
		atsStore:      atsStore,
	}
	server.verbosity.Store(int32(verbosity))
	server.state.Store(int32(ServerStateRunning))

	// Dedicated read connection for pulse API reads
	openPulseReadDB(server)

	SetDefaultServer(server)

	// Plugin manager debug log
	serverLogger.Debugw("Plugin manager check",
		"plugin_manager_is_nil", server.pluginManager == nil,
		"services_is_nil", server.services == nil)

	// Run subsystems in order
	for _, entry := range subsystems {
		if err := entry.sub.Init(server); err != nil {
			switch entry.policy {
			case SubsystemFatal:
				cancel()
				return nil, errors.Wrapf(err, "subsystem %s failed", entry.sub.Name())
			case SubsystemWarn:
				serverLogger.Warnw("Subsystem init failed (non-fatal)",
					"subsystem", entry.sub.Name(), "error", err)
			}
		}
	}

	return server, nil
}

// createServerLogger creates a named server logger from the global logger.
func createServerLogger(_ int) *zap.SugaredLogger {
	return logger.Logger.Desugar().Named("server").Sugar()
}

// createServerDependencies creates all components needed for QNTXServer initialization.
func createServerDependencies(db *sql.DB, cfg *appcfg.Config, serverLogger *zap.SugaredLogger) (*serverDependencies, error) {
	start := time.Now()

	usageTracker := tracker.NewUsageTracker(db, 0)

	budgetTracker := budget.NewTracker(db, budget.BudgetConfig{
		DailyBudgetUSD:          cfg.Pulse.DailyBudgetUSD,
		WeeklyBudgetUSD:         cfg.Pulse.WeeklyBudgetUSD,
		MonthlyBudgetUSD:        cfg.Pulse.MonthlyBudgetUSD,
		CostPerScoreUSD:         cfg.Pulse.CostPerScoreUSD,
		ClusterDailyBudgetUSD:   cfg.Pulse.ClusterDailyBudgetUSD,
		ClusterWeeklyBudgetUSD:  cfg.Pulse.ClusterWeeklyBudgetUSD,
		ClusterMonthlyBudgetUSD: cfg.Pulse.ClusterMonthlyBudgetUSD,
	})

	daemonStart := time.Now()
	daemon := async.NewWorkerPool(db, cfg, async.DefaultWorkerPoolConfig(), serverLogger)
	serverLogger.Debugw("Daemon created", "duration_ms", time.Since(daemonStart).Milliseconds())

	pluginManager := grpcplugin.GetDefaultPluginManager()

	serverLogger.Debugw("All dependencies created", "total_duration_ms", time.Since(start).Milliseconds())

	return &serverDependencies{
		usageTracker:  usageTracker,
		budgetTracker: budgetTracker,
		daemon:        daemon,
		pluginManager: pluginManager,
		config:        cfg,
	}, nil
}

// setupConfigWatcher sets up config file watching with reload callbacks.
func setupConfigWatcher(server *QNTXServer, db *sql.DB, serverLogger *zap.SugaredLogger) {
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
	appcfg.SetGlobalWatcher(configWatcher)

	configWatcher.OnReload(func(newCfg *appcfg.Config) error {
		serverLogger.Infow("Config reloaded, updating budget tracker",
			"daily_budget", newCfg.Pulse.DailyBudgetUSD,
			"weekly_budget", newCfg.Pulse.WeeklyBudgetUSD,
			"monthly_budget", newCfg.Pulse.MonthlyBudgetUSD,
		)
		server.budgetTracker = budget.NewTracker(db, budget.BudgetConfig{
			DailyBudgetUSD:          newCfg.Pulse.DailyBudgetUSD,
			WeeklyBudgetUSD:         newCfg.Pulse.WeeklyBudgetUSD,
			MonthlyBudgetUSD:        newCfg.Pulse.MonthlyBudgetUSD,
			CostPerScoreUSD:         newCfg.Pulse.CostPerScoreUSD,
			ClusterDailyBudgetUSD:   newCfg.Pulse.ClusterDailyBudgetUSD,
			ClusterWeeklyBudgetUSD:  newCfg.Pulse.ClusterWeeklyBudgetUSD,
			ClusterMonthlyBudgetUSD: newCfg.Pulse.ClusterMonthlyBudgetUSD,
		})
		server.broadcastDaemonStatus()
		return nil
	})

	configWatcher.OnReload(func(newCfg *appcfg.Config) error {
		manager := grpcplugin.GetDefaultPluginManager()
		registry := plugin.GetDefaultRegistry()
		if manager == nil || registry == nil {
			serverLogger.Warnw("Plugin hot-swap skipped: manager or registry not initialized",
				"manager_nil", manager == nil, "registry_nil", registry == nil)
			return nil
		}

		nowEnabled := make(map[string]bool, len(newCfg.Plugin.Enabled))
		for _, name := range newCfg.Plugin.Enabled {
			nowEnabled[name] = true
		}
		currentlyLoaded := make(map[string]bool)
		for _, name := range manager.LoadedPluginNames() {
			currentlyLoaded[name] = true
		}

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

	configWatcher.Start()
	serverLogger.Infow("Config watcher started", "path", configPath)
}
