package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/cmd/qntx/commands"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/qntx-code"
	"github.com/teranos/QNTX/logger"
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

// initializePluginRegistry sets up the domain plugin registry
func initializePluginRegistry() {
	// Create registry with QNTX version
	registry := domains.NewRegistry("0.1.0")
	domains.SetDefaultRegistry(registry)

	// Register built-in code domain plugin
	codePlugin := code.NewPlugin()
	if err := registry.Register(codePlugin); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to register code plugin: %v\n", err)
		os.Exit(1)
	}
}

// addPluginCommands adds commands from all registered plugins
func addPluginCommands() {
	registry := domains.GetDefaultRegistry()
	if registry == nil {
		return
	}

	for _, name := range registry.List() {
		plugin, ok := registry.Get(name)
		if !ok {
			continue
		}

		// Add plugin commands to root
		for _, cmd := range plugin.Commands() {
			rootCmd.AddCommand(cmd)
		}
	}
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
