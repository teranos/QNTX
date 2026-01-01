package util

import (
	"go/ast"
	"reflect"
	"strings"
)

// JSONTagInfo holds parsed information from a json struct tag
type JSONTagInfo struct {
	Name      string // Field name from json tag
	Omitempty bool   // Has omitempty option
	Skip      bool   // Skip this field (json:"-")
}

// ParseJSONTag extracts json tag information from a struct field tag.
// Returns nil if there's no json tag.
func ParseJSONTag(tag *ast.BasicLit) *JSONTagInfo {
	if tag == nil {
		return nil
	}

	// Remove backticks
	tagValue := strings.Trim(tag.Value, "`")
	st := reflect.StructTag(tagValue)

	// Parse json tag
	jsonTag := st.Get("json")
	if jsonTag == "" {
		return nil
	}

	info := &JSONTagInfo{}
	parts := strings.Split(jsonTag, ",")
	info.Name = parts[0]

	if info.Name == "-" {
		info.Skip = true
		return info
	}

	for _, part := range parts[1:] {
		if part == "omitempty" {
			info.Omitempty = true
		}
	}

	return info
}

// ParseCustomTag extracts a custom tag (like rusttype, tstype) from a struct field tag.
// Returns name, options map, and skip boolean.
func ParseCustomTag(tag *ast.BasicLit, tagName string) (name string, options map[string]bool, skip bool) {
	if tag == nil {
		return "", nil, false
	}

	// Remove backticks
	tagValue := strings.Trim(tag.Value, "`")
	st := reflect.StructTag(tagValue)

	customTag := st.Get(tagName)
	if customTag == "" {
		return "", nil, false
	}

	if customTag == "-" {
		return "", nil, true
	}

	parts := strings.Split(customTag, ",")
	name = parts[0]
	options = make(map[string]bool)

	for _, part := range parts[1:] {
		options[part] = true
	}

	return name, options, false
}
