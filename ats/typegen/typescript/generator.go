package typescript

import (
	"fmt"
	"go/ast"
	"reflect"
	"sort"
	"strings"
)

// Result holds the generated TypeScript for all types in a package.
// This matches the typegen.Result interface.
type Result struct {
	Types       map[string]string
	PackageName string
}

// FieldTagInfo contains parsed struct tag information for TypeScript generation
type FieldTagInfo struct {
	JSONName   string // Field name from json tag
	Omitempty  bool   // Has omitempty option
	TSType     string // Custom TypeScript type from tstype tag
	TSOptional bool   // Force optional with tstype:",optional"
	Skip       bool   // Skip this field (json:"-" or tstype:"-")
}

// ParseFieldTags extracts json and tstype tags from a struct field tag.
// Exported for testing.
func ParseFieldTags(tag *ast.BasicLit) FieldTagInfo {
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

// Generator implements typegen.Generator for TypeScript
type Generator struct{}

// NewGenerator creates a new TypeScript generator
func NewGenerator() *Generator {
	return &Generator{}
}

// Language returns "typescript"
func (g *Generator) Language() string {
	return "typescript"
}

// FileExtension returns "ts"
func (g *Generator) FileExtension() string {
	return "ts"
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

// GenerateInterface creates a TypeScript interface from a Go struct
func GenerateInterface(name string, structType *ast.StructType) string {
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
			tagInfo := ParseFieldTags(field.Tag)

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

// GenerateUnionType creates a TypeScript union type from const values
func GenerateUnionType(name string, values []string) string {
	var parts []string
	for _, v := range values {
		parts = append(parts, fmt.Sprintf("'%s'", v))
	}
	return fmt.Sprintf("export type %s = %s;", name, strings.Join(parts, " | "))
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
func (g *Generator) GenerateFile(result *Result) string {
	var sb strings.Builder

	sb.WriteString("/* eslint-disable */\n")
	sb.WriteString("// Code generated by ats/typegen from Go source. DO NOT EDIT.\n")
	sb.WriteString("// Regenerate with: make types\n")
	sb.WriteString(fmt.Sprintf("// Source package: %s\n\n", result.PackageName))

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
