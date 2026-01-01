package rust

import (
	"fmt"
	"go/ast"
	"reflect"
	"sort"
	"strings"

	"github.com/teranos/QNTX/ats/typegen"
)

// FieldTagInfo contains parsed struct tag information for Rust generation
type FieldTagInfo struct {
	JSONName   string // Field name from json tag
	Omitempty  bool   // Has omitempty option (maps to Option<T>)
	RustType   string // Custom Rust type from rusttype tag
	RustOption bool   // Force Option<T> with rusttype:",optional"
	Skip       bool   // Skip this field (json:"-" or rusttype:"-")
}

// ParseFieldTags extracts json and rusttype tags from a struct field tag.
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

	// Parse rusttype tag
	rusttypeTag := st.Get("rusttype")
	if rusttypeTag != "" {
		if rusttypeTag == "-" {
			info.Skip = true
			return info
		}
		parts := strings.Split(rusttypeTag, ",")
		info.RustType = parts[0]
		for _, part := range parts[1:] {
			if part == "optional" {
				info.RustOption = true
			}
		}
	}

	return info
}

// Generator implements typegen.Generator for Rust
type Generator struct{}

// NewGenerator creates a new Rust generator
func NewGenerator() *Generator {
	return &Generator{}
}

// Language returns "rust"
func (g *Generator) Language() string {
	return "rust"
}

// FileExtension returns "rs"
func (g *Generator) FileExtension() string {
	return "rs"
}

// GenerateInterface converts a Go struct to a Rust struct (implements typegen.Generator)
func (g *Generator) GenerateInterface(name string, structType *ast.StructType) string {
	return GenerateStruct(name, structType)
}

// GenerateUnionType converts const values to a Rust enum (implements typegen.Generator)
func (g *Generator) GenerateUnionType(name string, values []string) string {
	return GenerateEnum(name, values)
}

// TypeMapping defines how Go types map to Rust types
var TypeMapping = map[string]string{
	"string":                 "String",
	"int":                    "i64",
	"int8":                   "i8",
	"int16":                  "i16",
	"int32":                  "i32",
	"int64":                  "i64",
	"uint":                   "u64",
	"uint8":                  "u8",
	"uint16":                 "u16",
	"uint32":                 "u32",
	"uint64":                 "u64",
	"float32":                "f32",
	"float64":                "f64",
	"bool":                   "bool",
	"time.Time":              "String", // RFC3339 string
	"time.Duration":          "i64",    // Milliseconds
	"json.RawMessage":        "serde_json::Value",
	"map[string]interface{}": "serde_json::Map<String, serde_json::Value>",
	// SQL nullable types - map to Option<T>
	"sql.NullString": "Option<String>",
	"sql.NullInt64":  "Option<i64>",
	"sql.NullInt32":  "Option<i32>",
	"sql.NullBool":   "Option<bool>",
	"sql.NullTime":   "Option<String>",
	"NullString":     "Option<String>",
	"NullInt64":      "Option<i64>",
	"NullTime":       "Option<String>",
}

// GenerateStruct creates a Rust struct from a Go struct
func GenerateStruct(name string, structType *ast.StructType) string {
	var sb strings.Builder

	sb.WriteString("#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]\n")
	sb.WriteString(fmt.Sprintf("pub struct %s {\n", name))

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

			// Parse struct tags (json and rusttype)
			tagInfo := ParseFieldTags(field.Tag)

			// Skip fields marked with json:"-" or rusttype:"-"
			if tagInfo.Skip {
				continue
			}

			// Determine field name (json tag or Go field name in snake_case)
			jsonName := tagInfo.JSONName
			if jsonName == "" {
				jsonName = toSnakeCase(fieldName.Name)
			}

			// Determine if field is optional
			isPointer := isPointerType(field.Type)
			isOptional := tagInfo.Omitempty || tagInfo.RustOption || isPointer

			// Get Rust type (rusttype tag overrides inferred type)
			var rustType string
			if tagInfo.RustType != "" {
				rustType = tagInfo.RustType
			} else {
				rustType = goTypeToRust(field.Type)
				// For pointer types without rusttype override, wrap in Option
				if isPointer && !strings.HasPrefix(rustType, "Option<") {
					rustType = "Option<" + rustType + ">"
				}
			}

			// Wrap in Option if field is optional and not already an Option
			if isOptional && !strings.HasPrefix(rustType, "Option<") {
				rustType = "Option<" + rustType + ">"
			}

			// Add serde rename if json name differs from Rust field name
			rustFieldName := toSnakeCase(fieldName.Name)
			if jsonName != rustFieldName {
				sb.WriteString(fmt.Sprintf("    #[serde(rename = \"%s\")]\n", jsonName))
			}

			// Add skip_serializing_if for optional fields
			if isOptional {
				sb.WriteString("    #[serde(skip_serializing_if = \"Option::is_none\")]\n")
			}

			// Extract and format comments
			comment := extractFieldComment(field)
			if comment != "" {
				sb.WriteString(fmt.Sprintf("    /// %s\n", comment))
			}

			sb.WriteString(fmt.Sprintf("    pub %s: %s,\n", toRustIdent(rustFieldName), rustType))
		}
	}

	sb.WriteString("}")

	return sb.String()
}

// extractFieldComment extracts and formats the comment from a field
func extractFieldComment(field *ast.Field) string {
	if field.Doc != nil && len(field.Doc.List) > 0 {
		// Use Doc comment (appears before the field)
		var lines []string
		for _, comment := range field.Doc.List {
			text := cleanCommentText(comment.Text)
			if text != "" {
				lines = append(lines, text)
			}
		}
		return strings.Join(lines, " ")
	}

	if field.Comment != nil && len(field.Comment.List) > 0 {
		// Use inline comment (appears after the field)
		return cleanCommentText(field.Comment.List[0].Text)
	}

	return ""
}

// cleanCommentText removes comment markers and trims whitespace
func cleanCommentText(text string) string {
	text = strings.TrimPrefix(text, "//")
	text = strings.TrimPrefix(text, "/**")
	text = strings.TrimPrefix(text, "/*")
	text = strings.TrimSuffix(text, "*/")
	return strings.TrimSpace(text)
}

// GenerateEnum creates a Rust enum from const values
func GenerateEnum(name string, values []string) string {
	// Sort values for deterministic output
	sort.Strings(values)

	var sb strings.Builder

	sb.WriteString("#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]\n")
	sb.WriteString(fmt.Sprintf("pub enum %s {\n", name))

	for _, v := range values {
		// Convert value to PascalCase variant name
		variantName := toPascalCase(v)
		sb.WriteString(fmt.Sprintf("    #[serde(rename = \"%s\")]\n", v))
		sb.WriteString(fmt.Sprintf("    %s,\n", variantName))
	}

	sb.WriteString("}")

	return sb.String()
}

// isPointerType checks if the AST expression represents a pointer type
func isPointerType(expr ast.Expr) bool {
	_, ok := expr.(*ast.StarExpr)
	return ok
}

// goTypeToRust converts a Go AST type expression to Rust type string
func goTypeToRust(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		// Basic type or type reference in same package
		if rs, ok := TypeMapping[t.Name]; ok {
			return rs
		}
		// Assume it's a reference to another type in the same package
		return t.Name

	case *ast.SelectorExpr:
		// Qualified type like time.Time
		if ident, ok := t.X.(*ast.Ident); ok {
			fullName := ident.Name + "." + t.Sel.Name
			if rs, ok := TypeMapping[fullName]; ok {
				return rs
			}
			// Unknown qualified type
			return t.Sel.Name
		}
		return "serde_json::Value"

	case *ast.StarExpr:
		// Pointer type - get the underlying type
		return goTypeToRust(t.X)

	case *ast.ArrayType:
		// Slice or array
		elemType := goTypeToRust(t.Elt)
		return "Vec<" + elemType + ">"

	case *ast.MapType:
		// Map type
		keyType := goTypeToRust(t.Key)
		valType := goTypeToRust(t.Value)

		// Special case for map[string]interface{}
		if keyType == "String" && valType == "serde_json::Value" {
			return "serde_json::Map<String, serde_json::Value>"
		}

		return fmt.Sprintf("std::collections::HashMap<%s, %s>", keyType, valType)

	case *ast.InterfaceType:
		// interface{} -> serde_json::Value
		return "serde_json::Value"

	default:
		return "serde_json::Value"
	}
}

// Rust keywords that need raw identifier prefix (r#)
var rustKeywords = map[string]bool{
	"as": true, "async": true, "await": true, "break": true, "const": true,
	"continue": true, "crate": true, "dyn": true, "else": true, "enum": true,
	"extern": true, "false": true, "fn": true, "for": true, "if": true,
	"impl": true, "in": true, "let": true, "loop": true, "match": true,
	"mod": true, "move": true, "mut": true, "pub": true, "ref": true,
	"return": true, "self": true, "Self": true, "static": true, "struct": true,
	"super": true, "trait": true, "true": true, "type": true, "unsafe": true,
	"use": true, "where": true, "while": true, "yield": true,
}

// toRustIdent converts an identifier to a valid Rust identifier
// Adds r# prefix for Rust keywords
func toRustIdent(s string) string {
	if rustKeywords[s] {
		return "r#" + s
	}
	return s
}

// toRustConstIdent converts an identifier to a valid Rust constant identifier (SCREAMING_SNAKE_CASE)
// Handles keyword escaping properly (r#AS not R#AS)
func toRustConstIdent(s string) string {
	snakeCase := toSnakeCase(s)
	if rustKeywords[snakeCase] {
		return "r#" + strings.ToUpper(snakeCase)
	}
	return strings.ToUpper(snakeCase)
}

// toSnakeCase converts PascalCase or camelCase to snake_case
// Handles acronyms like "ID" -> "id" not "i_d"
func toSnakeCase(s string) string {
	var result strings.Builder
	runes := []rune(s)

	for i := 0; i < len(runes); i++ {
		r := runes[i]

		// Check if we need to insert underscore before this character
		if i > 0 && r >= 'A' && r <= 'Z' {
			// Don't insert underscore if previous char was uppercase (acronym)
			// unless next char is lowercase (end of acronym)
			prevUpper := runes[i-1] >= 'A' && runes[i-1] <= 'Z'
			nextLower := i+1 < len(runes) && runes[i+1] >= 'a' && runes[i+1] <= 'z'

			if !prevUpper || nextLower {
				result.WriteRune('_')
			}
		}

		result.WriteRune(r)
	}

	return strings.ToLower(result.String())
}

// toPascalCase converts snake_case or kebab-case to PascalCase
func toPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-'
	})
	var result strings.Builder
	for _, part := range parts {
		if len(part) > 0 {
			result.WriteString(strings.ToUpper(string(part[0])))
			if len(part) > 1 {
				result.WriteString(part[1:])
			}
		}
	}
	return result.String()
}

// sortedKeys returns map keys as a sorted slice
func sortedKeys[K ~string, V any](m map[K]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	return keys
}

// extractTypeReferences extracts all type names referenced in Rust type strings
// For example: "Option<Job>" -> ["Job"], "Vec<Execution>" -> ["Execution"]
func extractTypeReferences(rustType string) []string {
	var refs []string

	// Remove wrapper types to get to the base type name
	cleaned := rustType
	cleaned = strings.ReplaceAll(cleaned, "Option<", "")
	cleaned = strings.ReplaceAll(cleaned, "Vec<", "")
	cleaned = strings.ReplaceAll(cleaned, ">", "")
	cleaned = strings.TrimSpace(cleaned)

	// Skip if it contains :: (qualified path like serde_json::Value)
	if strings.Contains(cleaned, "::") {
		return refs
	}

	// Skip if it's a primitive/standard type
	primitives := map[string]bool{
		"String": true, "i64": true, "f64": true, "bool": true,
	}

	if !primitives[cleaned] && cleaned != "" {
		// Could be a custom type reference
		refs = append(refs, cleaned)
	}

	return refs
}

// collectExternalTypes finds all types referenced but not defined in this package
func collectExternalTypes(result *typegen.Result) []string {
	externalTypes := make(map[string]bool)
	definedTypes := make(map[string]bool)

	// Mark all types defined in this package
	for typeName := range result.Types {
		definedTypes[typeName] = true
	}

	// Scan all struct fields for type references
	for _, typeCode := range result.Types {
		// Extract field type declarations from the generated Rust code
		// Look for patterns like "pub field_name: TypeName," or "pub field_name: Option<TypeName>,"
		lines := strings.Split(typeCode, "\n")
		for _, line := range lines {
			if !strings.Contains(line, "pub ") || !strings.Contains(line, ":") {
				continue
			}

			// Extract the type part after the colon
			parts := strings.SplitN(line, ":", 2)
			if len(parts) != 2 {
				continue
			}

			typeStr := strings.TrimSpace(parts[1])
			typeStr = strings.TrimSuffix(typeStr, ",")

			// Extract type references from this type string
			refs := extractTypeReferences(typeStr)
			for _, ref := range refs {
				// Only add if it's not defined in current package
				if !definedTypes[ref] {
					externalTypes[ref] = true
				}
			}
		}
	}

	// Convert to sorted slice
	var types []string
	for typeName := range externalTypes {
		types = append(types, typeName)
	}
	sort.Strings(types)

	return types
}

// GenerateFile creates a complete Rust file from a typegen.Result
func (g *Generator) GenerateFile(result *typegen.Result) string {
	var sb strings.Builder

	// Header with generation metadata
	sb.WriteString("// Code generated by ats/typegen from Go source. DO NOT EDIT.\n")
	sb.WriteString("// Regenerate with: make types\n")
	sb.WriteString(fmt.Sprintf("// Source package: %s\n", result.PackageName))
	sb.WriteString(fmt.Sprintf("// Generated at: %s\n", typegen.GetTimestamp()))
	if hash := typegen.GetGitHash(); hash != "" {
		sb.WriteString(fmt.Sprintf("// Source version: %s\n", hash))
	}
	sb.WriteString("\n")

	// Module-level documentation
	sb.WriteString(fmt.Sprintf("//! # %s module\n", result.PackageName))
	sb.WriteString("//!\n")
	sb.WriteString(fmt.Sprintf("//! Generated from Go package: %s\n", result.PackageName))
	sb.WriteString("//!\n")
	sb.WriteString("//! This module contains auto-generated type definitions.\n")
	sb.WriteString("//! All types include serde Serialize/Deserialize traits for JSON compatibility.\n")
	sb.WriteString("\n")

	// Lint suppressions for generated code
	sb.WriteString("#![allow(clippy::all)]\n")
	sb.WriteString("#![allow(unused_imports)]\n")
	sb.WriteString("\n")

	// Generate imports for external types
	externalTypes := collectExternalTypes(result)
	if len(externalTypes) > 0 {
		sb.WriteString("use crate::{")
		for i, typeName := range externalTypes {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(typeName)
		}
		sb.WriteString("};\n\n")
	}

	// Generate const declarations (untyped consts)
	if len(result.Consts) > 0 {
		for _, name := range sortedKeys(result.Consts) {
			value := result.Consts[name]
			rustName := toRustConstIdent(name)
			sb.WriteString(fmt.Sprintf("pub const %s: &str = \"%s\";\n", rustName, value))
		}
		sb.WriteString("\n")
	}

	// Sort type names for deterministic output
	names := sortedKeys(result.Types)
	for i, name := range names {
		// Add documentation link as #[doc] attribute (preferred for generated code)
		// Convert type name to markdown anchor (lowercase with hyphens for multi-word names)
		anchor := strings.ToLower(strings.ReplaceAll(toSnakeCase(name), "_", ""))
		docLink := fmt.Sprintf("https://github.com/teranos/QNTX/blob/main/docs/types/%s.md#%s", result.PackageName, anchor)
		sb.WriteString(fmt.Sprintf("#[doc = \"Documentation: <%s>\"]\n", docLink))

		sb.WriteString(result.Types[name])
		if i < len(names)-1 {
			sb.WriteString("\n\n")
		}
	}

	if len(result.Types) > 0 {
		sb.WriteString("\n")
	}

	// Generate array constants (slice literals)
	if len(result.Arrays) > 0 {
		if len(result.Types) > 0 || len(result.Consts) > 0 {
			sb.WriteString("\n")
		}

		for _, name := range sortedKeys(result.Arrays) {
			elements := result.Arrays[name]

			// Check if all elements are const references
			allConsts := true
			for _, elem := range elements {
				if !typegen.IsConstReference(elem, result.Consts) {
					allConsts = false
					break
				}
			}

			if allConsts {
				// Use const references (uppercase)
				rustElements := make([]string, len(elements))
				for i, elem := range elements {
					rustElements[i] = toRustConstIdent(elem)
				}
				sb.WriteString(fmt.Sprintf("pub const %s: &[&str] = &[%s];\n",
					toRustConstIdent(name), strings.Join(rustElements, ", ")))
			} else {
				// Use string literals
				rustElements := make([]string, len(elements))
				for i, elem := range elements {
					if typegen.IsConstReference(elem, result.Consts) {
						rustElements[i] = toRustConstIdent(elem)
					} else {
						rustElements[i] = fmt.Sprintf("\"%s\"", elem)
					}
				}
				sb.WriteString(fmt.Sprintf("pub const %s: &[&str] = &[%s];\n",
					toRustConstIdent(name), strings.Join(rustElements, ", ")))
			}
		}
	}

	// Generate map constants (map literals)
	if len(result.Maps) > 0 {
		if len(result.Types) > 0 || len(result.Consts) > 0 || len(result.Arrays) > 0 {
			sb.WriteString("\n")
		}

		for _, name := range sortedKeys(result.Maps) {
			mapData := result.Maps[name]

			// For Rust, we'll use lazy_static for const maps (uppercase)
			sb.WriteString("lazy_static::lazy_static! {\n")
			sb.WriteString(fmt.Sprintf("    pub static ref %s: std::collections::HashMap<&'static str, &'static str> = {\n",
				toRustConstIdent(name)))
			sb.WriteString("        let mut m = std::collections::HashMap::new();\n")

			// Sort map keys for deterministic output
			for _, key := range sortedKeys(mapData) {
				value := mapData[key]
				keyStr := formatMapKey(key, result.Consts)
				valueStr := formatMapValue(value, result.Consts)
				sb.WriteString(fmt.Sprintf("        m.insert(%s, %s);\n", keyStr, valueStr))
			}

			sb.WriteString("        m\n")
			sb.WriteString("    };\n")
			sb.WriteString("}\n")
		}
	}

	if len(result.Types) > 0 || len(result.Consts) > 0 || len(result.Arrays) > 0 || len(result.Maps) > 0 {
		sb.WriteString("\n")
	}

	return sb.String()
}

// formatMapKey formats a map key for Rust output
func formatMapKey(key string, consts map[string]string) string {
	if typegen.IsConstReference(key, consts) {
		return toRustConstIdent(key)
	}
	return "\"" + key + "\""
}

// formatMapValue formats a map value for Rust output
func formatMapValue(value string, consts map[string]string) string {
	if typegen.IsConstReference(value, consts) {
		return toRustConstIdent(value)
	}
	return "\"" + value + "\""
}
