package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/cmd/qntx/commands"
	"github.com/teranos/QNTX/internal/version"
	"github.com/teranos/QNTX/logger"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc"
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
  as     - Create attestation assertions
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
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Initialize global logger before any command runs
		// Skip for commands that don't need logging output (like 'am show')
		if cmd.Name() != "show" {
			if err := logger.Initialize(false); err != nil {
				return fmt.Errorf("failed to initialize logger: %w", err)
			}
		}
		return nil
	},
}

func init() {
	// Initialize logger early for plugin loading
	// Use human-readable output for better UX
	if err := logger.Initialize(false); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to initialize logger: %v\n", err)
	}

	// Initialize domain plugin registry
	initializePluginRegistry()

	// Add global flags
	rootCmd.PersistentFlags().CountP("verbose", "v", "Increase output verbosity (repeat for more detail: -v, -vv, -vvv)")

	// Add commands
	rootCmd.AddCommand(commands.AmCmd)
	rootCmd.AddCommand(commands.AsCmd)
	rootCmd.AddCommand(commands.AxCmd)
	// CodeCmd now provided by code domain plugin
	rootCmd.AddCommand(commands.DbCmd)
	rootCmd.AddCommand(commands.HandlerCmd)
	rootCmd.AddCommand(commands.PulseCmd)
	rootCmd.AddCommand(commands.IxCmd)
	rootCmd.AddCommand(commands.ServerCmd)
	rootCmd.AddCommand(commands.TypegenCmd)
	rootCmd.AddCommand(commands.VersionCmd)

	// Add domain plugin commands
	addPluginCommands()
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

	// If no plugins enabled, run in minimal mode
	if len(cfg.Plugin.Enabled) == 0 {
		pluginLogger.Infow("No plugins enabled - QNTX running in minimal core mode")
		return
	}

	// Pre-register plugin names immediately so routes can be registered
	for _, pluginName := range cfg.Plugin.Enabled {
		registry.PreRegister(pluginName)
	}
	pluginLogger.Infof("Pre-registered %d plugins, loading in background", len(cfg.Plugin.Enabled))

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

	manager, err := grpc.LoadPluginsFromConfig(ctx, cfg, pluginLogger)
	if err != nil {
		pluginLogger.Errorw("Failed to load plugins from configuration", "error", err)
		return
	}

	// Store manager globally for server access
	grpc.SetDefaultPluginManager(manager)

	// Register loaded plugins with registry
	loadedPlugins := manager.GetAllPlugins()
	pluginLogger.Infof("Registering %d loaded plugins with registry", len(loadedPlugins))
	for i, p := range loadedPlugins {
		meta := p.Metadata()
		pluginLogger.Infof("[%d/%d] Attempting to register '%s' plugin", i+1, len(loadedPlugins), meta.Name)
		if err := registry.Register(p); err != nil {
			pluginLogger.Errorf("Failed to register '%s' plugin v%s: %s (skipping)",
				meta.Name, meta.Version, err.Error())
			continue
		}
		// Mark plugin as running since gRPC connection is already established
		registry.MarkReady(meta.Name)
		pluginLogger.Infof("Registered '%s' plugin v%s - %s",
			meta.Name, meta.Version, meta.Description)
	}

	pluginLogger.Info("Plugin loading complete")

	// CRITICAL: Initialize all loaded plugins now that they're registered
	// This must happen HERE (not in server/init.go) because plugins load asynchronously
	// and the server starts before plugin loading completes.
	// Get the server's service registry (this is a bit hacky but necessary for async loading)
	defaultServer := server.GetDefaultServer()
	if defaultServer != nil && defaultServer.GetServices() != nil {
		pluginLogger.Infow("Initializing loaded plugins with services", "count", len(loadedPlugins))
		if err := registry.InitializeAll(context.Background(), defaultServer.GetServices()); err != nil {
			pluginLogger.Errorw("Failed to initialize plugins", "error", err)
		} else {
			pluginLogger.Infow("Successfully initialized all plugins")
		}
	} else {
		pluginLogger.Warnw("Cannot initialize plugins - server or services not available yet")
	}
}

// addPluginCommands was used to add commands from all registered plugins.
// This is no longer needed as plugin commands are not integrated into the CLI.
// Plugins should provide their own CLI binaries.
// Domain functionality is exposed via the server API.
func addPluginCommands() {
	// No-op: Plugin commands are no longer registered
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
