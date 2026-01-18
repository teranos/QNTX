package typescript

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/teranos/QNTX/errors"
)

// PackageExport represents a package and its exported types for barrel export
type PackageExport struct {
	PackageName string
	TypeNames   []string
}

// GenerateIndexFile creates a barrel export file (index.ts) for cleaner imports
func GenerateIndexFile(outputDir string, exports []PackageExport) error {
	var sb strings.Builder

	// Header
	sb.WriteString("/* eslint-disable */\n")
	sb.WriteString("// Auto-generated barrel export - re-exports all generated types\n")
	sb.WriteString("// This file is regenerated on every `make types` run\n\n")

	// Sort packages for deterministic output
	sort.Slice(exports, func(i, j int) bool {
		return exports[i].PackageName < exports[j].PackageName
	})

	// Generate exports for each package
	for _, exp := range exports {
		if len(exp.TypeNames) == 0 {
			continue
		}

		// Sort type names
		sortedTypes := make([]string, len(exp.TypeNames))
		copy(sortedTypes, exp.TypeNames)
		sort.Strings(sortedTypes)

		sb.WriteString(fmt.Sprintf("// Types from %s\n", exp.PackageName))
		sb.WriteString(fmt.Sprintf("export type {\n"))
		for _, typeName := range sortedTypes {
			sb.WriteString(fmt.Sprintf("  %s,\n", typeName))
		}
		sb.WriteString(fmt.Sprintf("} from './%s';\n\n", exp.PackageName))
	}

	// Write index file
	indexPath := filepath.Join(outputDir, "index.ts")
	if err := os.WriteFile(indexPath, []byte(sb.String()), 0644); err != nil {
		return errors.Wrap(err, "failed to write index.ts")
	}

	fmt.Printf("✓ Generated %s (barrel export)\n", indexPath)
	return nil
}
