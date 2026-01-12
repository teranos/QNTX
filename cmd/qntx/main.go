package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/cmd/qntx/commands"
	"github.com/teranos/QNTX/logger"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc"
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
	rootCmd.AddCommand(commands.PulseCmd)
	rootCmd.AddCommand(commands.IxCmd)
	rootCmd.AddCommand(commands.ServerCmd)
	rootCmd.AddCommand(commands.TypegenCmd)
	rootCmd.AddCommand(commands.VersionCmd)

	// Add domain plugin commands
	addPluginCommands()
}

// initializePluginRegistry sets up the domain plugin registry with plugin discovery
func initializePluginRegistry() {
	// Initialize logger for plugin loading
	pluginLogger := logger.Logger.Named("plugin-loader")

	// Create registry with QNTX version and logger
	registry := plugin.NewRegistry("0.1.0", pluginLogger)
	plugin.SetDefaultRegistry(registry)

	// Load configuration to determine which plugins to load
	cfg, err := am.Load()
	if err != nil {
		// If config fails to load, warn but continue with no plugins
		pluginLogger.Warnw("Failed to load configuration, no plugins will be loaded", "error", err)
		return
	}

	// Load plugin-specific configs from ~/.qntx/plugins/*.toml
	// This merges [config] sections into am's viper for plugin initialization
	if err := am.LoadPluginConfigs(cfg.Plugin.Paths); err != nil {
		// Log detailed error but don't fail - plugins may still work with defaults
		pluginLogger.Warnw("Plugin configuration errors detected", "error", err)
	}

	// If no plugins enabled, run in minimal mode
	if len(cfg.Plugin.Enabled) == 0 {
		pluginLogger.Infow("No plugins enabled - QNTX running in minimal core mode")
		return
	}

	// Load plugins from configuration
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	manager, err := grpc.LoadPluginsFromConfig(ctx, cfg, pluginLogger)
	if err != nil {
		pluginLogger.Errorw("Failed to load plugins from configuration", "error", err)
		os.Exit(1)
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
			pluginLogger.Errorf("Failed to register '%s' plugin v%s: %s (may be duplicate or route conflict)",
				meta.Name, meta.Version, err.Error())
			os.Exit(1)
		}
		pluginLogger.Infof("Registered '%s' plugin v%s - %s",
			meta.Name, meta.Version, meta.Description)
	}
}

// addPluginCommands was used to add commands from all registered plugins.
// This is no longer needed as plugin commands are not integrated into the CLI.
// External plugins should provide their own CLI binaries.
// Built-in domain functionality is exposed via the server API.
func addPluginCommands() {
	// No-op: Plugin commands are no longer registered
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
