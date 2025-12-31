// Package typegen generates type definitions from Go source code for multiple target languages.
//
// This is QNTX's own type generator, designed to work with the attestation
// type system and maintain consistency between Go and multiple target languages
// (TypeScript, Python, Rust, Dart).
//
// # Struct Tag Support
//
// The generator recognizes standard json tags:
//
//	json:"fieldName,omitempty" - Sets field name and makes it optional
//
// Language-specific tags (e.g., tstype for TypeScript) are also supported.
// See language-specific generator documentation for details.
package typegen

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"

	"golang.org/x/tools/go/packages"
)

// ExcludedTypes are types that should not be generated (internal implementation details).
// Use "package.Type" format for package-specific exclusions.
var ExcludedTypes = map[string]bool{
	// pulse/async internal types
	"JobScanArgs":        true, // Database scan helper
	"HandlerRegistry":    true, // Internal registry
	"Store":              true, // Database store interface
	"Queue":              true, // Internal queue
	"WorkerPool":         true, // Internal pool
	"RegistryExecutor":   true,
	"JobProgressEmitter": true,
	// pulse/schedule internal types
	"ExecutionStore": true,       // Database store
	"Ticker":         true,       // Internal scheduler
	"TickerConfig":   true,       // Internal config
	"schedule.Job":   true,       // No json tags, PascalCase fields - use async.Job instead
	// server internal types
	"Client":              true, // WebSocket client
	"ConsoleBuffer":       true, // Internal buffer
	"StorageEventsPoller": true, // Internal poller
	"GraphViewState":      true, // Internal state
	"GLSPHandler":         true, // Internal handler
	"QNTXServer":          true, // Internal server
}

// GenerateFromPackage parses a Go package and generates type definitions
// for all exported struct types using the provided generator.
//
// Import path should be a full Go import path like "github.com/teranos/QNTX/ats/types"
func GenerateFromPackage(importPath string, gen Generator) (*Result, error) {
	// Load the package
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax | packages.NeedTypesInfo,
	}

	pkgs, err := packages.Load(cfg, importPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load package %s: %w", importPath, err)
	}

	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no packages found for %s", importPath)
	}

	pkg := pkgs[0]
	if len(pkg.Errors) > 0 {
		var errorMsgs []string
		for _, pkgErr := range pkg.Errors {
			errorMsgs = append(errorMsgs, pkgErr.Error())
		}
		return nil, fmt.Errorf("package %s has %d error(s):\n  - %s",
			importPath, len(pkg.Errors), strings.Join(errorMsgs, "\n  - "))
	}

	// Create language-agnostic result
	result := &Result{
		Types:       make(map[string]string),
		PackageName: pkg.Name,
	}

	// Process all files in the package
	for _, file := range pkg.Syntax {
		processFile(file, result, pkg.Name, gen)
	}

	return result, nil
}

// processFile extracts type definitions from a Go AST file using the provided generator.
func processFile(file *ast.File, result *Result, packageName string, gen Generator) {
	// First pass: collect type aliases (e.g., type JobStatus string)
	typeAliases := make(map[string]string) // typeName -> underlying type

	// Second pass: collect const values for each type
	constValues := make(map[string][]string) // typeName -> []values

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.GenDecl:
			if node.Tok == token.CONST {
				// Process const block
				processConstBlock(node, constValues)
			}
		case *ast.TypeSpec:
			// Only process exported types
			if !node.Name.IsExported() {
				return true
			}

			switch t := node.Type.(type) {
			case *ast.StructType:
				// Skip excluded types (internal implementation details)
				// Check both simple name and package-qualified name
				qualifiedName := packageName + "." + node.Name.Name
				if ExcludedTypes[node.Name.Name] || ExcludedTypes[qualifiedName] {
					return true
				}
				// Generate interface using the provided generator
				typeStr := gen.GenerateInterface(node.Name.Name, t)
				result.Types[node.Name.Name] = typeStr

			case *ast.Ident:
				// Type alias like: type JobStatus string
				typeAliases[node.Name.Name] = t.Name
			}
		}
		return true
	})

	// Generate union types for type aliases that have const values
	for typeName, underlyingType := range typeAliases {
		values, hasConsts := constValues[typeName]
		if hasConsts && len(values) > 0 && underlyingType == "string" {
			// Generate union type using the provided generator
			typeStr := gen.GenerateUnionType(typeName, values)
			result.Types[typeName] = typeStr
		}
	}
}

// processConstBlock extracts const values grouped by their type.
// It modifies constValues in-place by appending values for each type.
func processConstBlock(decl *ast.GenDecl, constValues map[string][]string) {
	var currentType string

	for _, spec := range decl.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}

		// Get the type of this const
		if valueSpec.Type != nil {
			if ident, ok := valueSpec.Type.(*ast.Ident); ok {
				currentType = ident.Name
			}
		}

		// Skip if we don't know the type
		if currentType == "" {
			continue
		}

		// Extract string literal values
		for _, value := range valueSpec.Values {
			if lit, ok := value.(*ast.BasicLit); ok && lit.Kind == token.STRING {
				// Remove quotes from string literal
				strValue := strings.Trim(lit.Value, `"`)
				constValues[currentType] = append(constValues[currentType], strValue)
			}
		}
	}
}
