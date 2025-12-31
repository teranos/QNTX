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
	"time.Duration":          "number",
	"json.RawMessage":        "unknown",
	"map[string]interface{}": "Record<string, unknown>",
	// SQL nullable types - map to TypeScript optional unions
	"sql.NullString": "string | null",
	"sql.NullInt64":  "number | null",
	"sql.NullInt32":  "number | null",
	"sql.NullBool":   "boolean | null",
	"sql.NullTime":   "string | null",
	"NullString":     "string | null",
	"NullInt64":      "number | null",
	"NullTime":       "string | null",
}

// ExcludedTypes are types that should not be generated (internal implementation details)
var ExcludedTypes = map[string]bool{
	"JobScanArgs":     true, // Database scan helper
	"HandlerRegistry": true, // Internal registry
	"Store":           true, // Database store interface
	"Queue":           true, // Internal queue
	"WorkerPool":      true, // Internal pool
	"RegistryExecutor": true,
	"JobProgressEmitter": true,
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
				if ExcludedTypes[node.Name.Name] {
					return true
				}
				// Generate TypeScript interface
				ts := generateInterface(node.Name.Name, t)
				result.Types[node.Name.Name] = ts

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
			// Generate union type from const values
			ts := generateUnionType(typeName, values)
			result.Types[typeName] = ts
		}
	}
}

// processConstBlock extracts const values grouped by their type
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

// generateUnionType creates a TypeScript union type from const values
func generateUnionType(name string, values []string) string {
	var parts []string
	for _, v := range values {
		parts = append(parts, fmt.Sprintf("'%s'", v))
	}
	return fmt.Sprintf("export type %s = %s;", name, strings.Join(parts, " | "))
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

			// Parse struct tags (json and tstype)
			tagInfo := parseFieldTags(field.Tag)

			// Skip fields marked with json:"-" or tstype:"-"
			if tagInfo.Skip {
				continue
			}

			// Determine field name (json tag or Go field name)
			jsonName := tagInfo.JSONName
			if jsonName == "" {
				jsonName = fieldName.Name
			}

			// Determine if field is optional
			isPointer := isPointerType(field.Type)
			isOptional := tagInfo.Omitempty || tagInfo.TSOptional || isPointer

			// Get TypeScript type (tstype tag overrides inferred type)
			var tsType string
			if tagInfo.TSType != "" {
				tsType = tagInfo.TSType
			} else {
				tsType = goTypeToTS(field.Type)
				// For pointer types without tstype override, add null union
				if isPointer {
					tsType = tsType + " | null"
				}
			}

			// Build field declaration
			optionalMark := ""
			if isOptional {
				optionalMark = "?"
			}

			sb.WriteString(fmt.Sprintf("  %s%s: %s;\n", jsonName, optionalMark, tsType))
		}
	}

	sb.WriteString("}")

	return sb.String()
}

// FieldTagInfo contains parsed struct tag information for TypeScript generation
type FieldTagInfo struct {
	JSONName   string // Field name from json tag
	Omitempty  bool   // Has omitempty option
	TSType     string // Custom TypeScript type from tstype tag
	TSOptional bool   // Force optional with tstype:",optional"
	Skip       bool   // Skip this field (json:"-" or tstype:"-")
}

// parseFieldTags extracts json and tstype tags from a struct field tag
//
// Supported tags:
//   - json:"name,omitempty" - Standard JSON field naming
//   - tstype:"CustomType" - Override TypeScript type
//   - tstype:"-" - Skip field in TypeScript output
//   - tstype:"Type,optional" - Override type and force optional
//
// Example:
//
//	Field string `json:"field" tstype:"string | null"`
func parseFieldTags(tag *ast.BasicLit) FieldTagInfo {
	info := FieldTagInfo{}

	if tag == nil {
		return info
	}

	// Remove backticks
	tagValue := strings.Trim(tag.Value, "`")
	st := reflect.StructTag(tagValue)

	// Parse json tag
	jsonTag := st.Get("json")
	if jsonTag != "" {
		parts := strings.Split(jsonTag, ",")
		info.JSONName = parts[0]
		if info.JSONName == "-" {
			info.Skip = true
			return info
		}
		for _, part := range parts[1:] {
			if part == "omitempty" {
				info.Omitempty = true
			}
		}
	}

	// Parse tstype tag
	tstypeTag := st.Get("tstype")
	if tstypeTag != "" {
		if tstypeTag == "-" {
			info.Skip = true
			return info
		}
		parts := strings.Split(tstypeTag, ",")
		info.TSType = parts[0]
		for _, part := range parts[1:] {
			if part == "optional" {
				info.TSOptional = true
			}
		}
	}

	return info
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
