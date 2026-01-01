// Package util provides shared utilities for type generators.
// It includes functions for parsing struct tags, extracting field comments,
// and performing case conversions between Go and target language conventions.
package util

import (
	"go/ast"
	"reflect"
	"strconv"
	"strings"
)

// NoConstraint is the sentinel value indicating no min/max constraint is set
const NoConstraint = -1

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

// ValidateTagInfo holds parsed information from a validate struct tag
type ValidateTagInfo struct {
	Required bool // Has required constraint
	Min      int  // Minimum value/length/items (NoConstraint if not set)
	Max      int  // Maximum value/length/items (NoConstraint if not set)
}

// ParseValidateTag extracts validation constraints from a validate tag.
// Supports: required, min=N, max=N
// Returns nil if there's no validate tag.
// Invalid values are silently ignored, allowing generators to handle
// malformed tags gracefully rather than failing the entire generation.
func ParseValidateTag(tag *ast.BasicLit) *ValidateTagInfo {
	if tag == nil {
		return nil
	}

	// Remove backticks
	tagValue := strings.Trim(tag.Value, "`")
	st := reflect.StructTag(tagValue)

	validateTag := st.Get("validate")
	if validateTag == "" {
		return nil
	}

	info := &ValidateTagInfo{
		Min: NoConstraint,
		Max: NoConstraint,
	}

	// Parse comma-separated constraints
	parts := strings.Split(validateTag, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)

		if part == "required" {
			info.Required = true
			continue
		}

		// Parse key=value constraints
		if strings.Contains(part, "=") {
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				continue
			}
			key := strings.TrimSpace(kv[0])
			valueStr := strings.TrimSpace(kv[1])

			switch key {
			case "min":
				if v, err := strconv.Atoi(valueStr); err == nil {
					info.Min = v
				}
			case "max":
				if v, err := strconv.Atoi(valueStr); err == nil {
					info.Max = v
				}
			}
		}
	}

	return info
}
