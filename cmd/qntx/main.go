package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/cmd/qntx/commands"
	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/server"
	"go.uber.org/zap"
)

var rootCmd = &cobra.Command{
	Use:   "qntx",
	Short: "QNTX - Attestation system and core infrastructure",
	Long: `QNTX - Attestation-based knowledge management and infrastructure.

QNTX provides core attestation system functionality, configuration management,
and infrastructure tools for building knowledge-based applications.

Available commands:
  am     - Manage QNTX core configuration ("I am")
  db     - Manage QNTX database operations
  pulse  - Manage Pulse daemon (async job processor + scheduler)
  ix     - Manage async ingestion jobs
  server - Start WebSocket graph visualization server

Examples:
  qntx am show             # Show current configuration
  qntx pulse start         # Start Pulse daemon
  qntx ix ls               # List async jobs
  qntx db stats            # Show database statistics
  qntx server              # Start graph visualization server`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// No subcommand: try Tauri desktop app first, fall back to server
		if tauriPath := findTauriBinary(); tauriPath != "" {
			proc := exec.Command(tauriPath)
			proc.Stdout = os.Stdout
			proc.Stderr = os.Stderr
			return proc.Run()
		}
		// No Tauri binary found — start the server directly
		return commands.ServerCmd.RunE(cmd, args)
	},
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Logger is already initialized in init() with file output.
		// Re-initializing here would overwrite the file-enabled logger
		// with a console-only logger, breaking server file logging.
		// Only initialize for commands that somehow run before init().
		if cmd.Name() == "show" {
			return nil
		}
		// Theme reload (Initialize already ran in init())
		return nil
	},
}

func init() {
	// Initialize logger early for plugin loading
	// Use human-readable output for better UX
	if err := logger.Initialize(false); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to initialize logger: %v\n", err)
	}

	// Add file output to the global logger before plugin loading starts.
	// This makes plugin-loader logs visible in the structured log file.
	if cfg, err := am.Load(); err == nil {
		logPath := cfg.GetLogPath(am.GetServerPort())
		if err := logger.AddFileOutput(logPath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: Failed to add file output to logger: %v\n", err)
		}
	}

	// Initialize domain plugin registry
	initializePluginRegistry()

	// Add global flags
	rootCmd.PersistentFlags().CountP("verbose", "v", "Increase output verbosity (repeat for more detail: -v, -vv, -vvv)")

	// Add commands
	rootCmd.AddCommand(commands.AmCmd)
	// CodeCmd now provided by code domain plugin
	rootCmd.AddCommand(commands.DbCmd)
	rootCmd.AddCommand(commands.HandlerCmd)
	rootCmd.AddCommand(commands.PulseCmd)
	rootCmd.AddCommand(commands.IxCmd)
	rootCmd.AddCommand(commands.ServerCmd)
	rootCmd.AddCommand(commands.VersionCmd)
}

// initializePluginRegistry sets up the domain plugin registry with async plugin discovery
// Returns immediately after pre-registering plugins, loads them in background
func initializePluginRegistry() {
	// Initialize logger for plugin loading
	pluginLogger := logger.Logger.Named("plugin-loader")

	// Create registry with QNTX version and logger
	registry := plugin.NewRegistry(version.VersionTag, pluginLogger)
	plugin.SetDefaultRegistry(registry)

	// Load configuration to determine which plugins to load
	cfg, err := am.Load()
	if err != nil {
		// If config fails to load, warn but continue with no plugins
		pluginLogger.Warnw("Failed to load configuration, no plugins will be loaded", "error", err)
		return
	}

	// Always create a plugin manager so hot-swap can enable plugins later via am.toml
	manager := grpc.NewPluginManager(pluginLogger, logger.Logger, cfg.Plugin.Runtime.TypeScriptRuntime)
	manager.SetAccumulator(grpc.NewPluginAccumulator(pluginLogger))
	if home, err := os.UserHomeDir(); err == nil {
		manager.SetPidFile(filepath.Join(home, ".qntx"), am.GetServerPort())
	}
	logPath := cfg.GetLogPath(am.GetServerPort())
	manager.SetLogDir(filepath.Dir(logPath))
	grpc.SetDefaultPluginManager(manager)

	// If no plugins enabled, run in minimal mode
	if len(cfg.Plugin.Enabled) == 0 {
		pluginLogger.Infow("No plugins enabled - QNTX running in minimal core mode")
		return
	}

	// Pre-register plugin names immediately so routes can be registered
	for _, pluginName := range cfg.Plugin.Enabled {
		registry.PreRegister(pluginName)
	}
	pluginLogger.Debugf("Pre-registered %d plugins, loading in background", len(cfg.Plugin.Enabled))

	// Plugin loading is deferred until the server signals it's ready.
	// The server calls onReady after migrations, routes, and HTTP listener
	// are all set up — no timeout polling, no race with migrations.
	commands.DeferredPluginInit = func() {
		loadPluginsAsync(cfg, pluginLogger, registry)
	}
}

// loadPluginsAsync performs async plugin loading without blocking server startup
func loadPluginsAsync(cfg *am.Config, pluginLogger *zap.SugaredLogger, registry *plugin.Registry) {
	// Load plugin-specific configs from ~/.qntx/plugins/*.toml
	if err := am.LoadPluginConfigs(cfg.Plugin.Paths); err != nil {
		pluginLogger.Warnw("Plugin configuration errors detected", "error", err)
	}

	// Load plugins into the existing manager (created in initializePluginRegistry)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	manager := grpc.GetDefaultPluginManager()
	if err := grpc.LoadPluginsFromConfig(ctx, manager, cfg, pluginLogger); err != nil {
		pluginLogger.Errorw("Failed to load plugins from configuration", "error", err)
		return
	}

	// Register loaded plugins with registry
	loadedPlugins := manager.GetAllPlugins()
	acc := manager.Accumulator()
	pluginLogger.Debugf("Registering %d loaded plugins with registry", len(loadedPlugins))
	registeredNames := make(map[string]bool)
	for i, p := range loadedPlugins {
		meta := p.Metadata()
		pluginLogger.Debugf("[%d/%d] Attempting to register '%s' plugin", i+1, len(loadedPlugins), meta.Name)
		if acc != nil {
			acc.SetLoading(meta.Name, meta.Version)
		}
		if err := registry.Register(p); err != nil {
			pluginLogger.Errorf("Failed to register '%s' plugin v%s: %s (skipping)",
				meta.Name, meta.Version, err.Error())
			registry.MarkFailed(meta.Name, err.Error())
			if acc != nil {
				acc.SetFailed(meta.Name, err.Error())
				acc.Emit(meta.Name, grpc.BannerBoot)
			}
			continue
		}
		// Mark plugin as running since gRPC connection is already established
		registry.MarkReady(meta.Name)
		registeredNames[meta.Name] = true
		pluginLogger.Debugf("Registered '%s' plugin v%s - %s",
			meta.Name, meta.Version, meta.Description)
	}

	// Mark any pre-registered plugins that never loaded as failed, with the real error
	failedErrors := manager.GetFailedPlugins()
	for _, name := range cfg.Plugin.Enabled {
		if registeredNames[name] {
			continue
		}
		if state, ok := registry.GetState(name); ok && state == plugin.StateLoading {
			reason := failedErrors[name]
			if reason == "" {
				reason = "plugin failed to load (check server logs for details)"
			}
			registry.MarkFailed(name, reason)
		}
	}

	pluginLogger.Debug("Plugin loading complete")

	// CRITICAL: Initialize all loaded plugins now that they're registered
	// This must happen HERE (not in server/init.go) because plugins load asynchronously
	// and the server starts before plugin loading completes.
	// Get the server's service registry (this is a bit hacky but necessary for async loading)
	defaultServer := server.GetDefaultServer()

	if defaultServer != nil && defaultServer.GetServices() != nil {
		services := defaultServer.GetServices()
		sm := defaultServer.GetServicesManager()

		// Wire watcher reload so plugin-declared watchers are loaded into the engine
		if pm := grpc.GetDefaultPluginManager(); pm != nil {
			pm.SetOnWatchersSetup(func() {
				if err := defaultServer.ReloadWatchers(); err != nil {
					pluginLogger.Warnw("Failed to reload watchers after plugin setup", "error", err)
				}
			})
			pm.SetOnPluginRestarted(func(name string) {
				defaultServer.InvalidatePluginMux(name)
				defaultServer.RegisterPluginMux(name)
			})
			pm.SetOnEmbeddingProviderReady(func(name string, client protocol.EmbeddingServiceClient) {
				defaultServer.SetupPluginEmbeddingService(client)
				pluginLogger.Debugw("Re-wired embedding provider after restart", "plugin", name)
			})
			pm.SetOnPythonProviderReady(func(name string, client protocol.PythonServiceClient) {
				defaultServer.AddPythonProvider(client)
				pluginLogger.Debugw("Registered Python provider after restart", "plugin", name)
			})
			groundDBPath := cfg.GroundDBPath
			pm.SetOnLifecycleEvent(func(pluginName, version, event string, routes []string) {
				ts := time.Now().Format("15:04:05")
				detail := fmt.Sprintf("%s %s %s at %s", pluginName, version, event, ts)
				if len(routes) > 0 {
					detail += " — " + strings.Join(routes, ", ")
				}
				attrs := map[string]interface{}{
					"event":          event,
					"plugin_version": version,
					"log_path":       filepath.Join(filepath.Dir(cfg.GetLogPath(am.GetServerPort())), pluginName+".log"),
				}
				if len(routes) > 0 {
					attrs["routes"] = routes
				}
				server.WriteImmediateNews(groundDBPath, pluginName, "plugin-lifecycle", pluginName, detail, attrs, pluginLogger)
			})
		}

		// Capture Pulse resources before init loop so goroutines can register handlers.
		daemon := defaultServer.GetDaemon()
		var handlerRegistry *async.HandlerRegistry
		var db *sql.DB
		if daemon != nil {
			handlerRegistry = daemon.Registry()
			db = defaultServer.GetDB()
			manager.SetPulseResources(db, handlerRegistry)
		}

		// Initialize all plugins concurrently — each plugin gets its own goroutine.
		// Each goroutine handles its own post-init work (providers, handlers, banner).
		// If Initialize doesn't respond within 30s, HTTP routes are pre-registered and
		// a partial banner is emitted; Initialize continues in the background.
		const initTimeout = 30 * time.Second
		pluginLogger.Debugw("Initializing plugins concurrently", "count", len(loadedPlugins))
		var initWg sync.WaitGroup
		for _, p := range loadedPlugins {
			initWg.Add(1)
			go func(p plugin.DomainPlugin) {
				defer initWg.Done()
				meta := p.Metadata()

				initCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
				initDone := make(chan error, 1)
				go func() {
					initDone <- p.Initialize(initCtx, services)
				}()

				var initErr error
				var timedOut bool
				select {
				case initErr = <-initDone:
					cancel()
				case <-time.After(initTimeout):
					timedOut = true
					pluginLogger.Warnw("Initialize not responding, continuing startup",
						"plugin", meta.Name, "timeout", initTimeout)
				}

				if timedOut {
					// Pre-register HTTP proxy routes so the plugin is reachable now
					defaultServer.RegisterPluginMux(meta.Name)

					// Emit partial banner so the UI shows something
					if acc != nil {
						acc.SetHealth(meta.Name, false, "initializing (waiting for gRPC response)", nil)
						acc.Emit(meta.Name, grpc.BannerBoot)
					}

					// Continue waiting in background — complete setup when Initialize returns
					go func() {
						defer cancel()
						bgErr := <-initDone
						if bgErr != nil {
							pluginLogger.Errorw("Background Initialize failed",
								"plugin", meta.Name, "version", meta.Version, "error", bgErr)
							registry.MarkFailed(meta.Name, bgErr.Error())
							if acc != nil {
								acc.SetFailed(meta.Name, bgErr.Error())
								acc.Emit(meta.Name, grpc.BannerBoot)
							}
							if pm := grpc.GetDefaultPluginManager(); pm != nil {
								pm.EmitLifecycle(meta.Name, meta.Version, "failed", nil)
							}
							return
						}
						pluginLogger.Infow("Background Initialize completed",
							"plugin", meta.Name, "version", meta.Version)
						registry.MarkReady(meta.Name)
						registerPluginProviders(p, meta, sm, defaultServer, pluginLogger, acc)
						registerPluginHandlers(p, meta, handlerRegistry, db, pluginLogger, acc)
						if err := defaultServer.ReloadWatchers(); err != nil {
							pluginLogger.Warnw("Failed to reload watchers after background init",
								"plugin", meta.Name, "error", err)
						}
					}()
					return
				}

				if initErr != nil {
					pluginLogger.Errorw("Failed to initialize plugin",
						"plugin", meta.Name, "version", meta.Version, "error", initErr)
					registry.MarkFailed(meta.Name, initErr.Error())
					if acc != nil {
						acc.SetFailed(meta.Name, initErr.Error())
						acc.Emit(meta.Name, grpc.BannerBoot)
					}
					return
				}

				registry.MarkReady(meta.Name)
				pluginLogger.Debugw("Initialized plugin", "plugin", meta.Name, "version", meta.Version)
				registerPluginProviders(p, meta, sm, defaultServer, pluginLogger, acc)
				registerPluginHandlers(p, meta, handlerRegistry, db, pluginLogger, acc)
			}(p)
		}
		initWg.Wait()

		// Reload watcher engine — plugins may have registered watchers during Initialize
		if err := defaultServer.ReloadWatchers(); err != nil {
			pluginLogger.Errorw("Failed to reload watchers after plugin init", "error", err)
		}

		if daemon == nil {
			pluginLogger.Warnw("Cannot register handlers - Pulse daemon not available, will retry")
			go retryPluginSetup(loadedPlugins, registry, pluginLogger, acc)
		} else {
			pluginLogger.Debugw("Plugin init complete")
		}
	} else {
		pluginLogger.Debugw("Cannot initialize plugins - server or services not available yet, will retry")
		go retryPluginSetup(loadedPlugins, registry, pluginLogger, acc)
	}

	// Start health polling — detect plugin crashes and restart automatically
	if defaultServer != nil && defaultServer.GetServices() != nil {
		manager.StartHealthPolling(registry, defaultServer.GetServices(), func(event grpc.HealthEvent) {
			defaultServer.BroadcastPluginHealth(event.Name, event.Healthy, event.State, event.Message)
		})
	}
}


// registerPluginProviders registers provider services (LLM, VectorSearch, Search, Embedding)
// for a plugin that has successfully completed Initialize.
func registerPluginProviders(p plugin.DomainPlugin, meta plugin.Metadata, sm *grpc.ServicesManager, srv *server.QNTXServer, logger *zap.SugaredLogger, acc *grpc.PluginAccumulator) {
	var roles []string
	if proxy, ok := p.(*grpc.ExternalDomainProxy); ok && sm != nil {
		if proxy.IsLLMProvider() {
			roles = append(roles, "llm-provider")
			if llmRouter := sm.GetLLMRouter(); llmRouter != nil {
				llmRouter.RegisterProvider(meta.Name, proxy.LLMServiceClient())
				logger.Debugw("Registered LLM provider", "plugin", meta.Name)
			}
		}
		if proxy.IsVectorSearchProvider() {
			roles = append(roles, "vector-search-provider")
			if vsRouter := sm.GetVectorSearchRouter(); vsRouter != nil {
				vsRouter.SetService(proxy.VectorSearchServiceClient())
				logger.Debugw("Registered VectorSearch provider", "plugin", meta.Name)
			}
		}
		if proxy.IsSearchProvider() {
			roles = append(roles, "search-provider")
			if searchRouter := sm.GetSearchRouter(); searchRouter != nil {
				searchRouter.RegisterProvider(meta.Name, proxy.SearchServiceClient())
				logger.Debugw("Registered Search provider", "plugin", meta.Name)
			}
		}
		if proxy.IsEmbeddingProvider() {
			roles = append(roles, "embedding-provider")
			srv.SetupPluginEmbeddingService(proxy.EmbeddingServiceClient())
			logger.Debugw("Registered embedding provider", "plugin", meta.Name)
		}
		if proxy.IsPythonProvider() {
			roles = append(roles, "python-provider")
			srv.AddPythonProvider(proxy.PythonServiceClient())
			logger.Debugw("Registered Python provider", "plugin", meta.Name)
		}
	}
	if acc != nil {
		acc.SetRoles(meta.Name, roles)
	}
}

// registerPluginHandlers registers Pulse async handlers/schedules and emits the plugin banner.
func registerPluginHandlers(p plugin.DomainPlugin, meta plugin.Metadata, handlerRegistry *async.HandlerRegistry, db *sql.DB, logger *zap.SugaredLogger, acc *grpc.PluginAccumulator) {
	externalPlugin, ok := p.(*grpc.ExternalDomainProxy)
	if !ok {
		return
	}
	if handlerRegistry != nil {
		for _, handlerName := range externalPlugin.GetHandlerNames() {
			logger.Debugw("Registering plugin async handler",
				"plugin", meta.Name, "handler", handlerName,
				"registry_key", grpc.PluginHandlerName(meta.Name, handlerName))
			proxyHandler := grpc.NewPluginProxyHandler(meta.Name, handlerName, externalPlugin, db, logger)
			handlerRegistry.Register(proxyHandler)
		}
	}

	schedules := externalPlugin.GetSchedules()
	if len(schedules) > 0 {
		if err := grpc.SetupPluginSchedules(db, meta.Name, schedules, logger); err != nil {
			logger.Errorw("Failed to setup plugin schedules",
				"plugin", meta.Name, "error", err)
		}
	}

	var routeStrs []string
	if routes := externalPlugin.GetHTTPRoutes(); len(routes) > 0 {
		routeStrs = make([]string, len(routes))
		for i, r := range routes {
			routeStrs[i] = r.GetMethod() + " " + r.GetPath()
		}
	}

	if acc != nil {
		acc.SetHandlers(meta.Name, externalPlugin.GetHandlerNames(), len(schedules), len(externalPlugin.GetWatchers()), grpc.CountUnfilteredWatchers(externalPlugin.GetWatchers()))
		if len(routeStrs) > 0 {
			acc.SetHTTPRoutes(meta.Name, routeStrs)
		}
		healthCtx, hCancel := context.WithTimeout(context.Background(), 5*time.Second)
		health := externalPlugin.Health(healthCtx)
		hCancel()
		details := make(map[string]string)
		for k, v := range health.Details {
			if s, ok := v.(string); ok {
				details[k] = s
			}
		}
		acc.SetHealth(meta.Name, health.Healthy, health.Message, details)
		acc.Emit(meta.Name, grpc.BannerBoot)
	}

	if pm := grpc.GetDefaultPluginManager(); pm != nil {
		pm.EmitLifecycle(meta.Name, meta.Version, "started", routeStrs)
	}
}

// retryPluginSetup waits for server infrastructure to be ready, then initializes plugins.
// Same inline init+register pattern as the primary path — no separate passes.
func retryPluginSetup(plugins []plugin.DomainPlugin, pluginRegistry *plugin.Registry, logger *zap.SugaredLogger, acc *grpc.PluginAccumulator) {
	for i := 0; i < 30; i++ {
		time.Sleep(1 * time.Second)

		defaultServer := server.GetDefaultServer()
		if defaultServer == nil {
			logger.Debugw("Waiting for server", "attempt", i+1, "blocked_by", "server not started")
			continue
		}

		services := defaultServer.GetServices()
		if services == nil {
			logger.Debugw("Waiting for server", "attempt", i+1, "blocked_by", "services not ready")
			continue
		}

		daemon := defaultServer.GetDaemon()
		if daemon == nil {
			logger.Debugw("Waiting for server", "attempt", i+1, "blocked_by", "daemon not ready")
			continue
		}

		sm := defaultServer.GetServicesManager()
		handlerRegistry := daemon.Registry()
		db := defaultServer.GetDB()

		// Initialize all plugins concurrently
		logger.Debugw("Server ready, initializing plugins concurrently", "count", len(plugins))
		var retryWg sync.WaitGroup
		for _, p := range plugins {
			retryWg.Add(1)
			go func(p plugin.DomainPlugin) {
				defer retryWg.Done()
				meta := p.Metadata()
				if acc != nil {
					acc.SetLoading(meta.Name, meta.Version)
				}
				initCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				if err := p.Initialize(initCtx, services); err != nil {
					logger.Errorw("Failed to initialize plugin",
						"plugin", meta.Name, "version", meta.Version, "error", err)
					pluginRegistry.MarkFailed(meta.Name, err.Error())
					if acc != nil {
						acc.SetFailed(meta.Name, err.Error())
						acc.Emit(meta.Name, grpc.BannerBoot)
					}
					return
				}
				pluginRegistry.MarkReady(meta.Name)
				logger.Debugw("Initialized plugin", "plugin", meta.Name, "version", meta.Version)
				registerPluginProviders(p, meta, sm, defaultServer, logger, acc)
				registerPluginHandlers(p, meta, handlerRegistry, db, logger, acc)
			}(p)
		}
		retryWg.Wait()

		if err := defaultServer.ReloadWatchers(); err != nil {
			logger.Errorw("Failed to reload watchers after plugin init", "error", err)
		}

		logger.Debugw("Plugin setup complete")
		return
	}

	defaultServer := server.GetDefaultServer()
	blocked := "server not started"
	if defaultServer != nil {
		if defaultServer.GetServices() == nil {
			blocked = "services not ready"
		} else if defaultServer.GetDaemon() == nil {
			blocked = "daemon not ready"
		}
	}
	logger.Errorw("Gave up waiting for server after 30 seconds", "blocked_by", blocked)
}

// findTauriBinary looks for the QNTX Tauri desktop binary.
// Checks: next to this binary, PATH, and platform-specific app locations.
func findTauriBinary() string {
	// Check next to the current executable
	if self, err := os.Executable(); err == nil {
		dir := filepath.Dir(self)
		candidates := tauriCandidates(dir)
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
	}

	// Check PATH
	if p, err := exec.LookPath(tauriBinaryName()); err == nil {
		return p
	}

	// Platform-specific app locations
	for _, c := range platformTauriPaths() {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}

	return ""
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		logger.Errorw("Fatal error", "error", err)
		os.Exit(1)
	}
}
