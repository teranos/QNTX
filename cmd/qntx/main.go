package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/cmd/qntx/commands"
	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
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
  ax     - Query attestations
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
	rootCmd.AddCommand(commands.AxCmd)
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

	// Load plugins asynchronously in background
	go loadPluginsAsync(cfg, pluginLogger, registry)
}

// loadPluginsAsync performs async plugin loading without blocking server startup
func loadPluginsAsync(cfg *am.Config, pluginLogger *zap.SugaredLogger, registry *plugin.Registry) {
	// Load plugin-specific configs from ~/.qntx/plugins/*.toml
	if err := am.LoadPluginConfigs(cfg.Plugin.Paths); err != nil {
		pluginLogger.Warnw("Plugin configuration errors detected", "error", err)
	}

	// Load plugins from configuration with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	manager, err := grpc.LoadPluginsFromConfig(ctx, cfg, pluginLogger, logger.Logger)
	if err != nil {
		pluginLogger.Errorw("Failed to load plugins from configuration", "error", err)
		return
	}

	// Store manager globally for server access
	grpc.SetDefaultPluginManager(manager)

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
			})
			pm.SetOnEmbeddingProviderReady(func(name string, client protocol.EmbeddingServiceClient) {
				defaultServer.SetupPluginEmbeddingService(client)
				pluginLogger.Debugw("Re-wired embedding provider after restart", "plugin", name)
			})
		}

		// Initialize each plugin individually, registering provider services
		// (LLM, VectorSearch) immediately after each init. This ensures provider
		// plugins like faiss make their services available before consumer plugins
		// like werf attempt to connect. No hardcoded ordering — alphabetical
		// sort is sufficient (faiss < werf).
		sorted := sortPluginsByName(loadedPlugins)
		pluginLogger.Debugw("Initializing plugins", "count", len(sorted))
		for _, p := range sorted {
			meta := p.Metadata()
			initCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			if err := p.Initialize(initCtx, services); err != nil {
				pluginLogger.Errorw("Failed to initialize plugin",
					"plugin", meta.Name, "version", meta.Version, "error", err)
				registry.MarkFailed(meta.Name, err.Error())
				if acc != nil {
					acc.SetFailed(meta.Name, err.Error())
					acc.Emit(meta.Name, grpc.BannerBoot)
				}
				cancel()
				continue
			}
			cancel()
			registry.MarkReady(meta.Name)
			pluginLogger.Debugw("Initialized plugin", "plugin", meta.Name, "version", meta.Version)

			// Collect provider roles for banner
			var roles []string

			// Provider services register immediately — before the next plugin inits
			if proxy, ok := p.(*grpc.ExternalDomainProxy); ok && sm != nil {
				if proxy.IsLLMProvider() {
					roles = append(roles, "llm-provider")
					if llmRouter := sm.GetLLMRouter(); llmRouter != nil {
						llmRouter.RegisterProvider(meta.Name, proxy.LLMServiceClient())
						pluginLogger.Debugw("Registered LLM provider", "plugin", meta.Name)
					}
				}
				if proxy.IsVectorSearchProvider() {
					roles = append(roles, "vector-search-provider")
					if vsRouter := sm.GetVectorSearchRouter(); vsRouter != nil {
						vsRouter.SetService(proxy.VectorSearchServiceClient())
						pluginLogger.Debugw("Registered VectorSearch provider", "plugin", meta.Name)
					}
				}
				if proxy.IsSearchProvider() {
					roles = append(roles, "search-provider")
					if searchRouter := sm.GetSearchRouter(); searchRouter != nil {
						searchRouter.RegisterProvider(meta.Name, proxy.SearchServiceClient())
						pluginLogger.Debugw("Registered Search provider", "plugin", meta.Name)
					}
				}
				if proxy.IsEmbeddingProvider() {
					roles = append(roles, "embedding-provider")
					defaultServer.SetupPluginEmbeddingService(proxy.EmbeddingServiceClient())
					pluginLogger.Debugw("Registered embedding provider", "plugin", meta.Name)
				}
			}

			if acc != nil {
				acc.SetRoles(meta.Name, roles)
			}
		}

		// Reload watcher engine — plugins may have registered watchers during Initialize
		if err := defaultServer.ReloadWatchers(); err != nil {
			pluginLogger.Errorw("Failed to reload watchers after plugin init", "error", err)
		}

		// Register plugin async handlers with Pulse and emit banners
		daemon := defaultServer.GetDaemon()
		if daemon != nil {
			handlerRegistry := daemon.Registry()
			db := defaultServer.GetDB()

			// Store Pulse resources so hot-restarts can re-register handlers/schedules
			manager.SetPulseResources(db, handlerRegistry)

			for _, p := range loadedPlugins {
				externalPlugin, ok := p.(*grpc.ExternalDomainProxy)
				if !ok {
					continue
				}

				meta := p.Metadata()
				for _, handlerName := range externalPlugin.GetHandlerNames() {
					pluginLogger.Debugw("Registering plugin async handler",
						"plugin", meta.Name, "handler", handlerName,
						"registry_key", grpc.PluginHandlerName(meta.Name, handlerName))
					proxyHandler := grpc.NewPluginProxyHandler(meta.Name, handlerName, externalPlugin, db, pluginLogger)
					handlerRegistry.Register(proxyHandler)
				}

				schedules := externalPlugin.GetSchedules()
				if len(schedules) > 0 {
					if err := grpc.SetupPluginSchedules(db, meta.Name, schedules, pluginLogger); err != nil {
						pluginLogger.Errorw("Failed to setup plugin schedules",
							"plugin", meta.Name, "error", err)
					}
				}

				// Accumulate handler/schedule/watcher counts and emit banner
				if acc != nil {
					acc.SetHandlers(meta.Name, externalPlugin.GetHandlerNames(), len(schedules), len(externalPlugin.GetWatchers()))
					if routes := externalPlugin.GetHTTPRoutes(); len(routes) > 0 {
						routeStrs := make([]string, len(routes))
						for i, r := range routes {
							routeStrs[i] = r.GetMethod() + " " + r.GetPath()
						}
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
			}
			pluginLogger.Debugw("Plugin async handler registration complete")
		} else {
			pluginLogger.Warnw("Cannot register handlers - Pulse daemon not available, will retry")
			go retryPluginSetup(loadedPlugins, registry, pluginLogger, acc)
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

// sortPluginsByName returns plugins sorted alphabetically by name.
// This gives deterministic init order without hardcoding dependencies —
// provider plugins (faiss, openrouter) naturally init before consumers (werf).
func sortPluginsByName(plugins []plugin.DomainPlugin) []plugin.DomainPlugin {
	sorted := make([]plugin.DomainPlugin, len(plugins))
	copy(sorted, plugins)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Metadata().Name < sorted[j].Metadata().Name
	})
	return sorted
}

// retryPluginSetup waits for server infrastructure to be ready, then initializes plugins.
// Same inline init+register pattern as the primary path — no separate passes.
func retryPluginSetup(plugins []plugin.DomainPlugin, pluginRegistry *plugin.Registry, logger *zap.SugaredLogger, acc *grpc.PluginAccumulator) {
	for i := 0; i < 30; i++ {
		time.Sleep(1 * time.Second)

		defaultServer := server.GetDefaultServer()
		if defaultServer == nil {
			continue
		}

		services := defaultServer.GetServices()
		if services == nil {
			continue
		}

		daemon := defaultServer.GetDaemon()
		if daemon == nil {
			continue
		}

		sm := defaultServer.GetServicesManager()

		// Init each plugin, register providers inline
		sorted := sortPluginsByName(plugins)
		logger.Debugw("Server ready, initializing plugins", "count", len(sorted))
		for _, p := range sorted {
			meta := p.Metadata()
			if acc != nil {
				acc.SetLoading(meta.Name, meta.Version)
			}
			initCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			if err := p.Initialize(initCtx, services); err != nil {
				logger.Errorw("Failed to initialize plugin",
					"plugin", meta.Name, "version", meta.Version, "error", err)
				pluginRegistry.MarkFailed(meta.Name, err.Error())
				if acc != nil {
					acc.SetFailed(meta.Name, err.Error())
					acc.Emit(meta.Name, grpc.BannerBoot)
				}
				cancel()
				continue
			}
			cancel()
			pluginRegistry.MarkReady(meta.Name)
			logger.Debugw("Initialized plugin", "plugin", meta.Name, "version", meta.Version)

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
					defaultServer.SetupPluginEmbeddingService(proxy.EmbeddingServiceClient())
					logger.Debugw("Registered embedding provider", "plugin", meta.Name)
				}
			}
			if acc != nil {
				acc.SetRoles(meta.Name, roles)
			}
		}

		// Register async handlers with Pulse
		handlerRegistry := daemon.Registry()
		db := defaultServer.GetDB()
		for _, p := range plugins {
			externalPlugin, ok := p.(*grpc.ExternalDomainProxy)
			if !ok {
				continue
			}
			meta := p.Metadata()
			for _, handlerName := range externalPlugin.GetHandlerNames() {
				logger.Debugw("Registering plugin async handler",
					"plugin", meta.Name, "handler", handlerName,
					"registry_key", grpc.PluginHandlerName(meta.Name, handlerName))
				proxyHandler := grpc.NewPluginProxyHandler(meta.Name, handlerName, externalPlugin, db, logger)
				handlerRegistry.Register(proxyHandler)
			}
			schedules := externalPlugin.GetSchedules()
			if len(schedules) > 0 {
				if err := grpc.SetupPluginSchedules(db, meta.Name, schedules, logger); err != nil {
					logger.Errorw("Failed to setup plugin schedules",
						"plugin", meta.Name, "error", err)
				}
			}

			if acc != nil {
				acc.SetHandlers(meta.Name, externalPlugin.GetHandlerNames(), len(schedules), len(externalPlugin.GetWatchers()))
				if routes := externalPlugin.GetHTTPRoutes(); len(routes) > 0 {
					routeStrs := make([]string, len(routes))
					for i, r := range routes {
						routeStrs[i] = r.GetMethod() + " " + r.GetPath()
					}
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
		}

		if err := defaultServer.ReloadWatchers(); err != nil {
			logger.Errorw("Failed to reload watchers after plugin init", "error", err)
		}

		logger.Debugw("Plugin setup complete")
		return
	}

	logger.Errorw("Gave up waiting for server after 30 seconds")
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
