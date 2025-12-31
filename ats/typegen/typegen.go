// Package typegen generates TypeScript type definitions from Go source code.
//
// This is QNTX's own type generator, designed to work with the attestation
// type system and maintain consistency between Go and TypeScript types.
package typegen

import (
	"fmt"
	"go/ast"
	"go/token"
	"reflect"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Result holds the generated TypeScript for all types in a package.
type Result struct {
	// Types maps Go type names to their TypeScript interface definitions
	Types map[string]string

	// PackageName is the Go package that was processed
	PackageName string
}

// TypeMapping defines how Go types map to TypeScript types
var TypeMapping = map[string]string{
	"string":                 "string",
	"int":                    "number",
	"int8":                   "number",
	"int16":                  "number",
	"int32":                  "number",
	"int64":                  "number",
	"uint":                   "number",
	"uint8":                  "number",
	"uint16":                 "number",
	"uint32":                 "number",
	"uint64":                 "number",
	"float32":                "number",
	"float64":                "number",
	"bool":                   "boolean",
	"time.Time":              "string",
	"json.RawMessage":        "unknown",
	"map[string]interface{}": "Record<string, unknown>",
}

// GenerateFromPackage parses a Go package and generates TypeScript interfaces
// for all exported struct types.
//
// Import path should be a full Go import path like "github.com/teranos/QNTX/ats/types"
func GenerateFromPackage(importPath string) (*Result, error) {
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
		return nil, fmt.Errorf("package errors: %v", pkg.Errors)
	}

	result := &Result{
		Types:       make(map[string]string),
		PackageName: pkg.Name,
	}

	// Process all files in the package
	for _, file := range pkg.Syntax {
		processFile(file, result)
	}

	return result, nil
}

// processFile extracts type definitions from a Go AST file
func processFile(file *ast.File, result *Result) {
	ast.Inspect(file, func(n ast.Node) bool {
		typeSpec, ok := n.(*ast.TypeSpec)
		if !ok {
			return true
		}

		// Only process exported types
		if !typeSpec.Name.IsExported() {
			return true
		}

		// Only process struct types
		structType, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			return true
		}

		// Generate TypeScript interface
		ts := generateInterface(typeSpec.Name.Name, structType)
		result.Types[typeSpec.Name.Name] = ts

		return true
	})
}

// generateInterface creates a TypeScript interface from a Go struct
func generateInterface(name string, structType *ast.StructType) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("export interface %s {\n", name))

	for _, field := range structType.Fields.List {
		if len(field.Names) == 0 {
			// Embedded field - skip for now
			continue
		}

		for _, fieldName := range field.Names {
			// Skip unexported fields
			if !fieldName.IsExported() {
				continue
			}

			// Get json tag
			jsonName, omitempty := parseJSONTag(field.Tag)
			if jsonName == "-" {
				continue // Skip fields with json:"-"
			}
			if jsonName == "" {
				jsonName = fieldName.Name
			}

			// Determine if field is optional (pointer or omitempty)
			isPointer := isPointerType(field.Type)
			isOptional := omitempty || isPointer

			// Get TypeScript type
			tsType := goTypeToTS(field.Type)

			// Build field declaration
			optionalMark := ""
			if isOptional {
				optionalMark = "?"
			}

			// For pointer types, add null union
			if isPointer && !omitempty {
				tsType = tsType + " | null"
			} else if isPointer && omitempty {
				tsType = tsType + " | null"
			}

			sb.WriteString(fmt.Sprintf("  %s%s: %s;\n", jsonName, optionalMark, tsType))
		}
	}

	sb.WriteString("}")

	return sb.String()
}

// parseJSONTag extracts the json field name and omitempty flag from a struct tag
func parseJSONTag(tag *ast.BasicLit) (name string, omitempty bool) {
	if tag == nil {
		return "", false
	}

	// Remove backticks
	tagValue := strings.Trim(tag.Value, "`")

	// Parse struct tag
	st := reflect.StructTag(tagValue)
	jsonTag := st.Get("json")

	if jsonTag == "" {
		return "", false
	}

	parts := strings.Split(jsonTag, ",")
	name = parts[0]

	for _, part := range parts[1:] {
		if part == "omitempty" {
			omitempty = true
		}
	}

	return name, omitempty
}

// isPointerType checks if the AST expression represents a pointer type
func isPointerType(expr ast.Expr) bool {
	_, ok := expr.(*ast.StarExpr)
	return ok
}

// goTypeToTS converts a Go AST type expression to TypeScript type string
func goTypeToTS(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		// Basic type or type reference in same package
		if ts, ok := TypeMapping[t.Name]; ok {
			return ts
		}
		// Assume it's a reference to another type in the same package
		return t.Name

	case *ast.SelectorExpr:
		// Qualified type like time.Time
		if ident, ok := t.X.(*ast.Ident); ok {
			fullName := ident.Name + "." + t.Sel.Name
			if ts, ok := TypeMapping[fullName]; ok {
				return ts
			}
			// Unknown qualified type
			return t.Sel.Name
		}
		return "unknown"

	case *ast.StarExpr:
		// Pointer type - get the underlying type
		return goTypeToTS(t.X)

	case *ast.ArrayType:
		// Slice or array
		elemType := goTypeToTS(t.Elt)
		return elemType + "[]"

	case *ast.MapType:
		// Map type
		keyType := goTypeToTS(t.Key)
		valType := goTypeToTS(t.Value)

		// Special case for map[string]interface{}
		if keyType == "string" && valType == "unknown" {
			return "Record<string, unknown>"
		}

		return fmt.Sprintf("Record<%s, %s>", keyType, valType)

	case *ast.InterfaceType:
		// interface{} -> unknown
		return "unknown"

	default:
		return "unknown"
	}
}

// GenerateFile creates a complete TypeScript file from a Result
func GenerateFile(result *Result) string {
	var sb strings.Builder

	sb.WriteString("// AUTO-GENERATED by ats/typegen - DO NOT EDIT\n")
	sb.WriteString(fmt.Sprintf("// Source: %s\n\n", result.PackageName))

	// Sort type names for deterministic output
	names := make([]string, 0, len(result.Types))
	for name := range result.Types {
		names = append(names, name)
	}
	sort.Strings(names)

	for i, name := range names {
		sb.WriteString(result.Types[name])
		if i < len(names)-1 {
			sb.WriteString("\n\n")
		}
	}

	sb.WriteString("\n")

	return sb.String()
}

// Ensure token is used (needed for packages.Load)
var _ = token.NoPos
