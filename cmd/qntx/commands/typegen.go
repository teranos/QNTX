package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/teranos/QNTX/ats/typegen"
)

// Default packages to generate types from
var defaultPackages = []string{
	"github.com/teranos/QNTX/ats/types",
	"github.com/teranos/QNTX/pulse/async",
	"github.com/teranos/QNTX/server",
}

var (
	typegenOutput   string
	typegenPackages []string
)

// TypegenCmd represents the typegen command
var TypegenCmd = &cobra.Command{
	Use:   "typegen",
	Short: "Generate TypeScript types from Go source",
	Long: `Generate TypeScript type definitions from Go structs.

This command parses Go source code and generates corresponding TypeScript
interfaces and type aliases. It handles:
  - Struct types → TypeScript interfaces
  - Type aliases with consts → TypeScript union types (enums)
  - JSON tags for field naming
  - Pointer types as optional fields
  - time.Time as string
  - map[string]interface{} as Record<string, unknown>

Examples:
  qntx typegen                           # Generate to stdout
  qntx typegen --output web/types/gen/   # Write to directory
  qntx typegen --packages pulse/async    # Specific package only`,
	RunE: runTypegen,
}

func init() {
	TypegenCmd.Flags().StringVarP(&typegenOutput, "output", "o", "", "Output directory (default: stdout)")
	TypegenCmd.Flags().StringSliceVarP(&typegenPackages, "packages", "p", nil, "Packages to process (default: ats/types, pulse/async, server)")
}

func runTypegen(cmd *cobra.Command, args []string) error {
	packages := typegenPackages
	if len(packages) == 0 {
		packages = defaultPackages
	}

	// Expand short package names to full import paths
	for i, pkg := range packages {
		if !strings.Contains(pkg, "/") {
			packages[i] = "github.com/teranos/QNTX/" + pkg
		} else if !strings.HasPrefix(pkg, "github.com/") {
			packages[i] = "github.com/teranos/QNTX/" + pkg
		}
	}

	for _, pkg := range packages {
		result, err := typegen.GenerateFromPackage(pkg)
		if err != nil {
			return fmt.Errorf("failed to generate types for %s: %w", pkg, err)
		}

		output := typegen.GenerateFile(result)

		if typegenOutput == "" {
			// Write to stdout
			fmt.Printf("// Package: %s\n", pkg)
			fmt.Println(output)
		} else {
			// Write to file
			filename := result.PackageName + ".ts"
			outputPath := filepath.Join(typegenOutput, filename)

			// Create directory if needed
			if err := os.MkdirAll(typegenOutput, 0755); err != nil {
				return fmt.Errorf("failed to create output directory: %w", err)
			}

			if err := os.WriteFile(outputPath, []byte(output), 0644); err != nil {
				return fmt.Errorf("failed to write %s: %w", outputPath, err)
			}

			fmt.Printf("✓ Generated %s (%d types)\n", outputPath, len(result.Types))
		}
	}

	return nil
}
