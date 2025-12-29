package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/cmd/qntx/commands"
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
}

func init() {
	// Add global flags
	rootCmd.PersistentFlags().CountP("verbose", "v", "Increase output verbosity (repeat for more detail: -v, -vv, -vvv)")

	// Add commands
	rootCmd.AddCommand(commands.AmCmd)
	rootCmd.AddCommand(commands.AsCmd)
	rootCmd.AddCommand(commands.DbCmd)
	rootCmd.AddCommand(commands.PulseCmd)
	rootCmd.AddCommand(commands.IxCmd)
	rootCmd.AddCommand(commands.ServerCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
