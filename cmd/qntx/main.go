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
  db     - Manage QNTX database operations
  pulse  - Manage Pulse daemon (async job processor + scheduler)
  ix     - Manage async ingestion jobs
  server - Start WebSocket graph visualization server

Future commands:
  ax     - Query attestations

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
	// Use silent mode to avoid cluttering output during init
	if err := logger.Initialize(true); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: Failed to initialize logger: %v\n", err)
	}

	// Initialize domain plugin registry
	initializePluginRegistry()

	// Add global flags
	rootCmd.PersistentFlags().CountP("verbose", "v", "Increase output verbosity (repeat for more detail: -v, -vv, -vvv)")

	// Add commands
	rootCmd.AddCommand(commands.AmCmd)
	rootCmd.AddCommand(commands.AsCmd)
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
	// Create registry with QNTX version
	registry := plugin.NewRegistry("0.1.0")
	plugin.SetDefaultRegistry(registry)

	// Initialize logger for plugin loading
	pluginLogger := logger.Logger.Named("plugin-loader")

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

	// Load plugins from configuration
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	manager, err := grpc.LoadPluginsFromConfig(ctx, cfg, pluginLogger)
	if err != nil {
		pluginLogger.Errorw("Failed to load plugins from configuration", "error", err)
		os.Exit(1)
	}

	// Register loaded plugins with registry
	loadedPlugins := manager.GetAllPlugins()
	for _, p := range loadedPlugins {
		if err := registry.Register(p); err != nil {
			pluginLogger.Errorw("Failed to register plugin",
				"plugin", p.Metadata().Name,
				"error", err,
			)
			os.Exit(1)
		}
		pluginLogger.Infow("Registered plugin with registry",
			"plugin", p.Metadata().Name,
			"version", p.Metadata().Version,
		)
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
