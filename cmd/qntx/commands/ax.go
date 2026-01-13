package commands

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/ats/parser"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/sym"
)

var (
	axLimit  int
	axFormat string
)

// AxCmd represents the ax command
var AxCmd = &cobra.Command{
	Use:   "ax [QUERY]",
	Short: sym.AX + " Query attestations",
	Long: sym.AX + ` ax — Query attestations

Query the attestation graph using natural grammar.

Examples:
  qntx ax                          # List all attestations
  qntx ax is engineer              # Find entities with predicate 'engineer'
  qntx ax of branch                # Find attestations in 'branch' context
  qntx ax ALICE is engineer        # Find specific subject with predicate

Grammar:
  - Subjects: Entity names to filter by
  - is/are: Keyword to filter by predicates
  - of: Keyword to filter by contexts
  - by: Keyword to filter by actors`,

	RunE: runAxCommand,
}

func init() {
	AxCmd.Flags().IntVarP(&axLimit, "limit", "l", 100, "Maximum number of results")
	AxCmd.Flags().StringVarP(&axFormat, "format", "f", "table", "Output format (table/json)")
}

func runAxCommand(cmd *cobra.Command, args []string) error {
	// Parse query
	filter, err := parser.ParseAxCommand(args)
	if err != nil {
		return errors.Wrap(err, "failed to parse query")
	}

	// Override limit if specified
	if axLimit > 0 {
		filter.Limit = axLimit
	}

	// Open database
	database, err := openDatabase("")
	if err != nil {
		return err
	}
	defer database.Close()

	// Create executor (uses default fuzzy matcher)
	executor := storage.NewExecutor(database)

	// Execute query
	result, err := executor.ExecuteAsk(context.Background(), *filter)
	if err != nil {
		return errors.Wrap(err, "failed to execute query")
	}

	// Display results
	if axFormat == "json" {
		return displayJSON(result)
	}

	return displayTable(result)
}

func displayTable(result *types.AxResult) error {
	fmt.Printf("%s Found %d attestations\n\n", sym.AX, len(result.Attestations))

	if len(result.Attestations) == 0 {
		return nil
	}

	for _, a := range result.Attestations {
		fmt.Printf("%s\n", a.ID)
		fmt.Printf("  Subjects:   %v\n", a.Subjects)
		fmt.Printf("  Predicates: %v\n", a.Predicates)
		if len(a.Contexts) > 0 {
			fmt.Printf("  Contexts:   %v\n", a.Contexts)
		}
		fmt.Printf("  Actors:     %v\n", a.Actors)
		fmt.Printf("\n")
	}

	return nil
}

func displayJSON(result *types.AxResult) error {
	output, err := json.MarshalIndent(result.Attestations, "", "  ")
	if err != nil {
		return errors.Wrap(err, "failed to marshal JSON")
	}
	fmt.Println(string(output))
	return nil
}
