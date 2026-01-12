package commands

import (
	"context"
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
	// Simple JSON output - just print the attestations
	fmt.Println("[")
	for i, a := range result.Attestations {
		comma := ","
		if i == len(result.Attestations)-1 {
			comma = ""
		}
		fmt.Printf("  {\"id\": %q, \"subjects\": %v, \"predicates\": %v, \"contexts\": %v, \"actors\": %v}%s\n",
			a.ID, a.Subjects, a.Predicates, a.Contexts, a.Actors, comma)
	}
	fmt.Println("]")
	return nil
}
