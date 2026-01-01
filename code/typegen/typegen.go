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
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

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
		Types:         make(map[string]string),
		PackageName:   pkg.Name,
		TypePositions: make(map[string]Position),
		TypeComments:  make(map[string]string),
		Consts:        make(map[string]string),
		Arrays:        make(map[string][]string),
		Maps:          make(map[string]map[string]string),
	}

	// Process all files in the package
	for _, file := range pkg.Syntax {
		processFile(file, result, pkg.Name, gen, pkg.Fset)
	}

	return result, nil
}

// processFile extracts type definitions from a Go AST file using the provided generator.
func processFile(file *ast.File, result *Result, packageName string, gen Generator, fset *token.FileSet) {
	// Capture source file path for documentation links (if not already set)
	if result.SourceFile == "" && file.Pos().IsValid() {
		pos := fset.Position(file.Pos())
		result.SourceFile = makeRelativePath(pos.Filename)
	}

	// First pass: collect type aliases (e.g., type JobStatus string)
	typeAliases := make(map[string]string) // typeName -> underlying type

	// Second pass: collect const values for each type
	constValues := make(map[string][]string) // typeName -> []values

	ast.Inspect(file, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.GenDecl:
			if node.Tok == token.CONST {
				// Process const block for typed consts (union types)
				processConstBlock(node, constValues)
				// Also process untyped consts (direct exports)
				processUntypedConsts(node, result)
			} else if node.Tok == token.VAR {
				// Process var declarations (slice and map literals)
				processVarDecls(node, result)
			} else if node.Tok == token.TYPE {
				// Process type declarations to extract doc comments
				for _, spec := range node.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						// Capture doc comment (prefer spec-level, fall back to general decl)
						if typeSpec.Doc != nil && typeSpec.Doc.Text() != "" {
							result.TypeComments[typeSpec.Name.Name] = strings.TrimSpace(typeSpec.Doc.Text())
						} else if node.Doc != nil && node.Doc.Text() != "" {
							result.TypeComments[typeSpec.Name.Name] = strings.TrimSpace(node.Doc.Text())
						}
					}
				}
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

				// Capture position for documentation links
				pos := fset.Position(node.Pos())
				result.TypePositions[node.Name.Name] = Position{
					File: makeRelativePath(pos.Filename),
					Line: pos.Line,
				}

			case *ast.Ident:
				// Type alias like: type JobStatus string
				typeAliases[node.Name.Name] = t.Name
			}
		}
		return true
	})

	// Generate union types for type aliases that have const values
	// We need to track positions for type aliases too
	ast.Inspect(file, func(n ast.Node) bool {
		if node, ok := n.(*ast.TypeSpec); ok {
			if node.Name.IsExported() {
				if _, isIdent := node.Type.(*ast.Ident); isIdent {
					typeName := node.Name.Name
					values, hasConsts := constValues[typeName]
					underlyingType := typeAliases[typeName]
					if hasConsts && len(values) > 0 && underlyingType == "string" {
						// Generate union type using the provided generator
						typeStr := gen.GenerateUnionType(typeName, values)
						result.Types[typeName] = typeStr

						// Capture position for documentation links
						pos := fset.Position(node.Pos())
						result.TypePositions[typeName] = Position{
							File: makeRelativePath(pos.Filename),
							Line: pos.Line,
						}
					}
				}
			}
		}
		return true
	})
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

// processUntypedConsts extracts untyped const declarations (const X = "value").
// These are exported directly as const values, not as union types.
func processUntypedConsts(decl *ast.GenDecl, result *Result) {
	for _, spec := range decl.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}

		// Only process untyped consts (no explicit type declaration)
		if valueSpec.Type != nil {
			continue
		}

		// Only process exported consts
		for i, name := range valueSpec.Names {
			if !name.IsExported() {
				continue
			}

			// Extract string literal values
			if i < len(valueSpec.Values) {
				if lit, ok := valueSpec.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					// Remove quotes from string literal
					strValue := strings.Trim(lit.Value, `"`)
					result.Consts[name.Name] = strValue
				}
			}
		}
	}
}

// processVarDecls extracts variable declarations with slice or map literals.
// e.g., var X = []string{...} or var Y = map[string]string{...}
func processVarDecls(decl *ast.GenDecl, result *Result) {
	for _, spec := range decl.Specs {
		valueSpec, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}

		// Only process exported vars
		for i, name := range valueSpec.Names {
			if !name.IsExported() {
				continue
			}

			// Extract slice or map literal values
			if i < len(valueSpec.Values) {
				switch lit := valueSpec.Values[i].(type) {
				case *ast.CompositeLit:
					// Check if it's a slice literal []string{...}
					if arrayType, ok := lit.Type.(*ast.ArrayType); ok {
						if ident, ok := arrayType.Elt.(*ast.Ident); ok && ident.Name == "string" {
							// Extract slice elements
							var elements []string
							for _, elt := range lit.Elts {
								if ident, ok := elt.(*ast.Ident); ok {
									// Element is a const reference (e.g., I, AM, IX)
									elements = append(elements, ident.Name)
								} else if lit, ok := elt.(*ast.BasicLit); ok && lit.Kind == token.STRING {
									// Element is a string literal
									strValue := strings.Trim(lit.Value, `"`)
									elements = append(elements, strValue)
								}
							}
							result.Arrays[name.Name] = elements
						}
					}

					// Check if it's a map literal map[string]string{...}
					if mapType, ok := lit.Type.(*ast.MapType); ok {
						if keyIdent, ok := mapType.Key.(*ast.Ident); ok && keyIdent.Name == "string" {
							if valIdent, ok := mapType.Value.(*ast.Ident); ok && valIdent.Name == "string" {
								// Extract map entries
								mapEntries := make(map[string]string)
								for _, elt := range lit.Elts {
									if kv, ok := elt.(*ast.KeyValueExpr); ok {
										// Extract key
										var key string
										switch k := kv.Key.(type) {
										case *ast.BasicLit:
											// Key is a string literal
											key = strings.Trim(k.Value, `"`)
										case *ast.Ident:
											// Key is a const reference
											key = k.Name
										}

										// Extract value
										var value string
										switch v := kv.Value.(type) {
										case *ast.BasicLit:
											// Value is a string literal
											value = strings.Trim(v.Value, `"`)
										case *ast.Ident:
											// Value is a const reference
											value = v.Name
										}

										if key != "" && value != "" {
											mapEntries[key] = value
										}
									}
								}
								result.Maps[name.Name] = mapEntries
							}
						}
					}
				}
			}
		}
	}
}

// makeRelativePath converts an absolute file path to a repository-relative path.
// For example: /Users/x/QNTX/ats/types/types.go -> ats/types/types.go
func makeRelativePath(absPath string) string {
	// Find the repository root by looking for go.mod
	dir := absPath
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root, fallback to filename
			return filepath.Base(absPath)
		}

		// Check if go.mod exists in this directory
		if _, err := os.Stat(filepath.Join(parent, "go.mod")); err == nil {
			// Found repo root, make path relative to it
			relPath, err := filepath.Rel(parent, absPath)
			if err != nil {
				return filepath.Base(absPath)
			}
			return relPath
		}

		dir = parent
	}
}

// GetTimestamp returns the current UTC timestamp in RFC3339 format for generated code headers
func GetTimestamp() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// GetGitHash returns the current git commit hash, or empty string if not in a git repo
func GetGitHash() string {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// GetLastCommitHash returns the hash of the last commit that modified the given file
// Returns empty string if file doesn't exist or not in git repo
func GetLastCommitHash(filepath string) string {
	cmd := exec.Command("git", "log", "-1", "--format=%h", "--", filepath)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// GetLastCommitTime returns the timestamp of the last commit that modified the given file
// Returns empty string if file doesn't exist or not in git repo
func GetLastCommitTime(filepath string) string {
	cmd := exec.Command("git", "log", "-1", "--format=%cI", "--", filepath)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}
