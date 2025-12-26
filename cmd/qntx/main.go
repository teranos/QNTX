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
  am    - Manage QNTX core configuration ("I am")

Future commands:
  as    - Create attestation assertions
  ax    - Query attestations
  ix    - Ingest data and generate attestations

Examples:
  qntx am show             # Show current configuration
  qntx am validate         # Validate configuration`,
}

func init() {
	// Add am command
	rootCmd.AddCommand(commands.AmCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
