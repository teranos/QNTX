package util

import (
	"go/ast"
)

// TypeConverterConfig configures how Go types are converted to target language types.
type TypeConverterConfig struct {
	// TypeMapping maps Go type names to target language types
	TypeMapping map[string]string

	// ArrayFormat formats an array type given the element type
	// e.g., Python: "list[%s]", Rust: "Vec<%s>", TypeScript: "%s[]"
	ArrayFormat func(elemType string) string

	// MapFormat formats a map type given key and value types
	// e.g., Python: "dict[%s, %s]", Rust: "std::collections::HashMap<%s, %s>"
	MapFormat func(keyType, valType string) string

	// StringMapUnknownType is the special type for map[string]interface{}
	// e.g., Python: "dict[str, Any]", Rust: "serde_json::Map<String, serde_json::Value>"
	StringMapUnknownType string

	// UnknownType is returned for interface{} and unrecognized types
	// e.g., Python: "Any", Rust: "serde_json::Value", TypeScript: "unknown"
	UnknownType string

	// StringType is the target language's string type (for map special case detection)
	StringType string
}

// ConvertGoType converts a Go AST type expression to a target language type string.
// The config parameter provides language-specific formatting rules.
func ConvertGoType(expr ast.Expr, config *TypeConverterConfig) string {
	switch t := expr.(type) {
	case *ast.Ident:
		// Go's `any` is an alias for interface{} — map to UnknownType
		if t.Name == "any" {
			return config.UnknownType
		}
		// Basic type or type reference in same package
		if mapped, ok := config.TypeMapping[t.Name]; ok {
			return mapped
		}
		// Assume it's a reference to another type in the same package
		return t.Name

	case *ast.SelectorExpr:
		// Qualified type like time.Time
		if ident, ok := t.X.(*ast.Ident); ok {
			fullName := ident.Name + "." + t.Sel.Name
			if mapped, ok := config.TypeMapping[fullName]; ok {
				return mapped
			}
			// Unknown qualified type - return just the type name
			return t.Sel.Name
		}
		return config.UnknownType

	case *ast.StarExpr:
		// Pointer type - get the underlying type
		return ConvertGoType(t.X, config)

	case *ast.ArrayType:
		// Slice or array
		elemType := ConvertGoType(t.Elt, config)
		return config.ArrayFormat(elemType)

	case *ast.MapType:
		// Map type
		keyType := ConvertGoType(t.Key, config)
		valType := ConvertGoType(t.Value, config)

		// Special case for map[string]interface{}
		if keyType == config.StringType && valType == config.UnknownType {
			return config.StringMapUnknownType
		}

		return config.MapFormat(keyType, valType)

	case *ast.InterfaceType:
		// interface{} -> unknown/Any type
		return config.UnknownType

	default:
		return config.UnknownType
	}
}
