package typescript

import (
	"fmt"
	"go/ast"
	"reflect"
	"sort"
	"strings"

	"github.com/teranos/QNTX/code/typegen"
	"github.com/teranos/QNTX/code/typegen/util"
)

// FieldTagInfo extends util.FieldTagInfo with TypeScript-specific fields
type FieldTagInfo struct {
	util.FieldTagInfo        // Embed shared tag info
	Readonly          bool   // Mark field as readonly (TypeScript-specific)
}

// ParseFieldTags extracts json and tstype tags from a struct field tag.
// Exported for testing. Uses shared util.ParseFieldTags with TypeScript-specific extensions.
func ParseFieldTags(tag *ast.BasicLit) FieldTagInfo {
	info := FieldTagInfo{
		FieldTagInfo: util.ParseFieldTags(tag, "tstype"),
	}

	// Parse readonly tag (TypeScript-specific)
	if tag != nil {
		tagValue := strings.Trim(tag.Value, "`")
		st := reflect.StructTag(tagValue)
		readonlyTag, ok := st.Lookup("readonly")
		if ok && readonlyTag != "false" {
			info.Readonly = true
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

// GenerateInterface converts a Go struct to a TypeScript interface (implements typegen.Generator)
func (g *Generator) GenerateInterface(name string, structType *ast.StructType) string {
	return GenerateInterface(name, structType)
}

// GenerateUnionType converts const values to a TypeScript union type (implements typegen.Generator)
func (g *Generator) GenerateUnionType(name string, values []string) string {
	return GenerateUnionType(name, values)
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
			isPointer := util.IsPointerType(field.Type)
			isOptional := tagInfo.Omitempty || tagInfo.CustomOptional || isPointer

			// Get TypeScript type (tstype tag overrides inferred type)
			var tsType string
			if tagInfo.CustomType != "" {
				tsType = tagInfo.CustomType
			} else {
				tsType = goTypeToTS(field.Type)
				// For pointer types without tstype override, add null union
				if isPointer {
					tsType = tsType + " | null"
				}
			}

			// Extract and format comments
			comment := util.ExtractFieldComment(field)

			// Parse validation constraints
			validateInfo := util.ParseValidateTag(field.Tag)

			// Build JSDoc comment with validation info
			if comment != "" || validateInfo != nil {
				sb.WriteString("  /**\n")
				if comment != "" {
					sb.WriteString(fmt.Sprintf("   * %s\n", comment))
				}
				if validateInfo != nil {
					if comment != "" {
						sb.WriteString("   *\n") // Blank line separator
					}
					// Add validation constraints as JSDoc tags
					if validateInfo.Required {
						sb.WriteString("   * @required\n")
					}
					if validateInfo.Min != util.NoConstraint {
						// Determine if it's array or string based on type
						if strings.HasSuffix(tsType, "[]") {
							sb.WriteString(fmt.Sprintf("   * @minItems %d\n", validateInfo.Min))
						} else if tsType == "string" {
							sb.WriteString(fmt.Sprintf("   * @minLength %d\n", validateInfo.Min))
						} else if tsType == "number" {
							sb.WriteString(fmt.Sprintf("   * @minimum %d\n", validateInfo.Min))
						}
					}
					if validateInfo.Max != util.NoConstraint {
						if strings.HasSuffix(tsType, "[]") {
							sb.WriteString(fmt.Sprintf("   * @maxItems %d\n", validateInfo.Max))
						} else if tsType == "string" {
							sb.WriteString(fmt.Sprintf("   * @maxLength %d\n", validateInfo.Max))
						} else if tsType == "number" {
							sb.WriteString(fmt.Sprintf("   * @maximum %d\n", validateInfo.Max))
						}
					}
				}
				sb.WriteString("   */\n")
			}

			// Build field declaration
			optionalMark := ""
			if isOptional {
				optionalMark = "?"
			}

			readonlyMark := ""
			if tagInfo.Readonly {
				readonlyMark = "readonly "
			}

			sb.WriteString(fmt.Sprintf("  %s%s%s: %s;\n", readonlyMark, jsonName, optionalMark, tsType))
		}
	}

	sb.WriteString("}")

	return sb.String()
}

// GenerateUnionType creates a TypeScript union type from const values
func GenerateUnionType(name string, values []string) string {
	// Sort values for deterministic output
	sort.Strings(values)

	var parts []string
	for _, v := range values {
		parts = append(parts, fmt.Sprintf("'%s'", v))
	}
	return fmt.Sprintf("export type %s = %s;", name, strings.Join(parts, " | "))
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

// GenerateFile creates a complete TypeScript file from a typegen.Result
func (g *Generator) GenerateFile(result *typegen.Result) string {
	var sb strings.Builder

	sb.WriteString("/* eslint-disable */\n")
	sb.WriteString("// Code generated by ats/typegen from Go source. DO NOT EDIT.\n")
	sb.WriteString("// Regenerate with: make types\n")
	sb.WriteString(fmt.Sprintf("// Source package: %s\n\n", result.PackageName))

	// Generate const exports (untyped consts like const I = "⍟")
	if len(result.Consts) > 0 {
		constNames := make([]string, 0, len(result.Consts))
		for name := range result.Consts {
			constNames = append(constNames, name)
		}
		sort.Strings(constNames)

		for _, name := range constNames {
			value := result.Consts[name]
			sb.WriteString(fmt.Sprintf("export const %s = \"%s\";\n", name, value))
		}
		sb.WriteString("\n")
	}

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

	if len(result.Types) > 0 {
		sb.WriteString("\n")
	}

	// Generate array exports (slice literals like var X = []string{...})
	if len(result.Arrays) > 0 {
		arrayNames := make([]string, 0, len(result.Arrays))
		for name := range result.Arrays {
			arrayNames = append(arrayNames, name)
		}
		sort.Strings(arrayNames)

		if len(result.Types) > 0 || len(result.Consts) > 0 {
			sb.WriteString("\n")
		}

		for _, name := range arrayNames {
			elements := result.Arrays[name]

			// Check if all elements are const references (for better type narrowing)
			allConsts := true
			for _, elem := range elements {
				if !typegen.IsConstReference(elem, result.Consts) {
					allConsts = false
					break
				}
			}

			if allConsts {
				// Use 'as const' for readonly tuple with literal types
				sb.WriteString(fmt.Sprintf("export const %s = [%s] as const;\n",
					name, strings.Join(elements, ", ")))
				// Generate union type for type checking
				sb.WriteString(fmt.Sprintf("export type %sSymbol = typeof %s[number];\n",
					name, name))
			} else {
				// Standard string array
				sb.WriteString(fmt.Sprintf("export const %s: string[] = [%s];\n",
					name, strings.Join(elements, ", ")))
			}
		}
	}

	// Generate map exports (map literals like var X = map[string]string{...})
	if len(result.Maps) > 0 {
		mapNames := make([]string, 0, len(result.Maps))
		for name := range result.Maps {
			mapNames = append(mapNames, name)
		}
		sort.Strings(mapNames)

		if len(result.Types) > 0 || len(result.Consts) > 0 || len(result.Arrays) > 0 {
			sb.WriteString("\n")
		}

		for _, name := range mapNames {
			mapData := result.Maps[name]
			sb.WriteString(fmt.Sprintf("export const %s: Record<string, string> = {\n", name))

			// Sort map keys for deterministic output
			keys := make([]string, 0, len(mapData))
			for k := range mapData {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			for i, key := range keys {
				value := mapData[key]
				// Check if key or value is a const reference by checking if it's in the Consts map
				keyStr := formatMapKeyWithConsts(key, result.Consts)
				valueStr := formatMapValueWithConsts(value, result.Consts)

				sb.WriteString(fmt.Sprintf("  %s: %s", keyStr, valueStr))
				if i < len(keys)-1 {
					sb.WriteString(",\n")
				} else {
					sb.WriteString("\n")
				}
			}

			sb.WriteString("};\n")
		}
	}

	if len(result.Types) > 0 || len(result.Consts) > 0 || len(result.Arrays) > 0 || len(result.Maps) > 0 {
		sb.WriteString("\n")
	}

	return sb.String()
}

// toTSComputedKey wraps a const name in brackets for computed property syntax
func toTSComputedKey(s string) string {
	return "[" + s + "]"
}

// identity returns the string unchanged (for TypeScript const values)
func identity(s string) string {
	return s
}

// formatMapKeyWithConsts formats a map key for TypeScript output.
// Const references are wrapped in brackets, literals are quoted.
func formatMapKeyWithConsts(key string, consts map[string]string) string {
	return typegen.FormatMapEntry(key, consts, toTSComputedKey)
}

// formatMapValueWithConsts formats a map value for TypeScript output.
// Const references are used as-is, literals are quoted.
func formatMapValueWithConsts(value string, consts map[string]string) string {
	return typegen.FormatMapEntry(value, consts, identity)
}
