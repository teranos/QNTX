package python

import (
	"fmt"
	"go/ast"
	"sort"
	"strings"

	"github.com/teranos/QNTX/typegen"
	"github.com/teranos/QNTX/typegen/util"
)

// ParseFieldTags extracts json and pytype tags from a struct field tag.
// Exported for testing. Uses shared util.ParseFieldTags internally.
func ParseFieldTags(tag *ast.BasicLit) util.FieldTagInfo {
	return util.ParseFieldTags(tag, "pytype")
}

// Generator implements typegen.Generator for Python
type Generator struct{}

// NewGenerator creates a new Python generator
func NewGenerator() *Generator {
	return &Generator{}
}

// Language returns "python"
func (g *Generator) Language() string {
	return "python"
}

// FileExtension returns "py"
func (g *Generator) FileExtension() string {
	return "py"
}

// GenerateInterface converts a Go struct to a Python dataclass (implements typegen.Generator)
func (g *Generator) GenerateInterface(name string, structType *ast.StructType) string {
	return GenerateDataclass(name, structType)
}

// GenerateUnionType converts const values to a Python Enum class (implements typegen.Generator)
func (g *Generator) GenerateUnionType(name string, values []string) string {
	return GenerateEnum(name, values)
}

// TypeMapping defines how Go types map to Python types
var TypeMapping = map[string]string{
	"string":                 "str",
	"int":                    "int",
	"int8":                   "int",
	"int16":                  "int",
	"int32":                  "int",
	"int64":                  "int",
	"uint":                   "int",
	"uint8":                  "int",
	"uint16":                 "int",
	"uint32":                 "int",
	"uint64":                 "int",
	"float32":                "float",
	"float64":                "float",
	"bool":                   "bool",
	"time.Time":              "str", // ISO8601/RFC3339 string
	"time.Duration":          "int", // Nanoseconds as int
	"json.RawMessage":        "Any",
	"map[string]interface{}": "dict[str, Any]",
	// SQL nullable types - map to Optional
	"sql.NullString": "str | None",
	"sql.NullInt64":  "int | None",
	"sql.NullInt32":  "int | None",
	"sql.NullBool":   "bool | None",
	"sql.NullTime":   "str | None",
	"NullString":     "str | None",
	"NullInt64":      "int | None",
	"NullTime":       "str | None",
}

// typeConverterConfig is the Python-specific type conversion configuration
var typeConverterConfig = &util.TypeConverterConfig{
	TypeMapping:          TypeMapping,
	ArrayFormat:          func(elem string) string { return "list[" + elem + "]" },
	MapFormat:            func(key, val string) string { return fmt.Sprintf("dict[%s, %s]", key, val) },
	StringMapUnknownType: "dict[str, Any]",
	UnknownType:          "Any",
	StringType:           "str",
}

// pythonKeywords are reserved words in Python that need special handling
var pythonKeywords = map[string]bool{
	"False": true, "None": true, "True": true, "and": true, "as": true,
	"assert": true, "async": true, "await": true, "break": true, "class": true,
	"continue": true, "def": true, "del": true, "elif": true, "else": true,
	"except": true, "finally": true, "for": true, "from": true, "global": true,
	"if": true, "import": true, "in": true, "is": true, "lambda": true,
	"nonlocal": true, "not": true, "or": true, "pass": true, "raise": true,
	"return": true, "try": true, "while": true, "with": true, "yield": true,
	// Soft keywords (Python 3.10+)
	"match": true, "case": true, "type": true,
}

// toPythonIdent converts an identifier to a valid Python identifier
// Adds underscore suffix for Python keywords
func toPythonIdent(s string) string {
	if pythonKeywords[s] {
		return s + "_"
	}
	return s
}

// GenerateDataclass creates a Python dataclass from a Go struct
func GenerateDataclass(name string, structType *ast.StructType) string {
	var sb strings.Builder

	sb.WriteString("@dataclass\n")
	sb.WriteString(fmt.Sprintf("class %s:\n", name))

	// Collect fields with their types and defaults
	type fieldInfo struct {
		name       string
		pyType     string
		isOptional bool
		comment    string
		validate   *util.ValidateTagInfo
	}

	var fields []fieldInfo
	hasAnyField := false

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

			// Parse struct tags (json and pytype)
			tagInfo := ParseFieldTags(field.Tag)

			// Skip fields marked with json:"-" or pytype:"-"
			if tagInfo.Skip {
				continue
			}

			hasAnyField = true

			// Determine field name (json tag or Go field name in snake_case)
			jsonName := tagInfo.JSONName
			if jsonName == "" {
				jsonName = util.ToSnakeCase(fieldName.Name)
			}

			// Determine if field is optional
			isPointer := util.IsPointerType(field.Type)
			isOptional := tagInfo.Omitempty || tagInfo.CustomOptional || isPointer

			// Get Python type (pytype tag overrides inferred type)
			var pyType string
			if tagInfo.CustomType != "" {
				pyType = tagInfo.CustomType
			} else {
				pyType = goTypeToPython(field.Type)
				// For pointer types without pytype override, add None union
				if isPointer && !strings.Contains(pyType, "None") {
					pyType = pyType + " | None"
				}
			}

			// Wrap in Optional if field is optional and not already containing None
			if isOptional && !strings.Contains(pyType, "None") {
				pyType = pyType + " | None"
			}

			// Extract and format comments
			comment := util.ExtractFieldComment(field)

			// Parse validation constraints
			validateInfo := util.ParseValidateTag(field.Tag)

			fields = append(fields, fieldInfo{
				name:       toPythonIdent(jsonName),
				pyType:     pyType,
				isOptional: isOptional,
				comment:    comment,
				validate:   validateInfo,
			})
		}
	}

	if !hasAnyField {
		sb.WriteString("    pass\n")
		return sb.String()
	}

	// Sort fields: required fields first, then optional with defaults
	// This is required by Python dataclasses
	sort.SliceStable(fields, func(i, j int) bool {
		// Required fields come before optional
		return !fields[i].isOptional && fields[j].isOptional
	})

	// Write fields
	for _, f := range fields {
		// Add docstring comment if present
		if f.comment != "" || f.validate != nil {
			sb.WriteString(fmt.Sprintf("    # %s", f.comment))
			if f.validate != nil {
				if f.comment != "" {
					sb.WriteString(" | ")
				}
				if f.validate.Required {
					sb.WriteString("required")
				}
				if f.validate.Min != util.NoConstraint {
					if f.validate.Required {
						sb.WriteString(", ")
					}
					sb.WriteString(fmt.Sprintf("min=%d", f.validate.Min))
				}
				if f.validate.Max != util.NoConstraint {
					if f.validate.Required || f.validate.Min != util.NoConstraint {
						sb.WriteString(", ")
					}
					sb.WriteString(fmt.Sprintf("max=%d", f.validate.Max))
				}
			}
			sb.WriteString("\n")
		}

		// Write field declaration
		if f.isOptional {
			sb.WriteString(fmt.Sprintf("    %s: %s = None\n", f.name, f.pyType))
		} else {
			sb.WriteString(fmt.Sprintf("    %s: %s\n", f.name, f.pyType))
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}

// GenerateEnum creates a Python Enum class from const values
// Uses (str, Enum) for JSON serialization compatibility
func GenerateEnum(name string, values []string) string {
	// Sort values for deterministic output
	sort.Strings(values)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("class %s(str, Enum):\n", name))

	for _, v := range values {
		// Convert value to SCREAMING_SNAKE_CASE for enum member name
		memberName := strings.ToUpper(util.ToSnakeCase(v))
		sb.WriteString(fmt.Sprintf("    %s = \"%s\"\n", memberName, v))
	}

	return strings.TrimRight(sb.String(), "\n")
}

// goTypeToPython converts a Go AST type expression to Python type string
func goTypeToPython(expr ast.Expr) string {
	return util.ConvertGoType(expr, typeConverterConfig)
}

// GenerateFile creates a complete Python file from a typegen.Result
func (g *Generator) GenerateFile(result *typegen.Result) string {
	var sb strings.Builder

	// Header with generation metadata
	sb.WriteString("# Code generated by typegen from Go source. DO NOT EDIT.\n")
	sb.WriteString("# Regenerate with: make types\n")
	sb.WriteString(fmt.Sprintf("# Source package: %s\n", result.PackageName))
	sb.WriteString("\n")

	// Module docstring
	sb.WriteString(fmt.Sprintf(`"""%s module

Generated from Go package: %s

This module contains auto-generated type definitions.
All types are Python dataclasses compatible with JSON serialization.
"""
`, result.PackageName, result.PackageName))
	sb.WriteString("\n")

	// Collect imports needed
	needsAny := false
	needsEnum := false
	needsDataclass := true

	// Check if we need Any or Enum
	for _, typeCode := range result.Types {
		if strings.Contains(typeCode, "Any") {
			needsAny = true
		}
		if strings.Contains(typeCode, "(str, Enum)") {
			needsEnum = true
		}
	}

	// Write imports
	sb.WriteString("from __future__ import annotations\n\n")

	if needsDataclass {
		sb.WriteString("from dataclasses import dataclass\n")
	}
	if needsEnum {
		sb.WriteString("from enum import Enum\n")
	}

	if needsAny {
		sb.WriteString("from typing import Any\n")
	}

	sb.WriteString("\n")

	// Generate const exports (untyped consts like const I = "...")
	if len(result.Consts) > 0 {
		constNames := make([]string, 0, len(result.Consts))
		for name := range result.Consts {
			constNames = append(constNames, name)
		}
		sort.Strings(constNames)

		for _, name := range constNames {
			value := result.Consts[name]
			// Python constant naming convention: SCREAMING_SNAKE_CASE
			pyName := strings.ToUpper(util.ToSnakeCase(name))
			sb.WriteString(fmt.Sprintf("%s = \"%s\"\n", pyName, value))
		}
		sb.WriteString("\n")
	}

	// Sort type names for deterministic output
	names := make([]string, 0, len(result.Types))
	for name := range result.Types {
		names = append(names, name)
	}
	sort.Strings(names)

	// Generate Enum classes first (union types from consts)
	for _, name := range names {
		typeCode := result.Types[name]
		if strings.HasPrefix(typeCode, "class "+name+"(str, Enum)") {
			// Add doc comment if available
			if docComment, hasComment := result.TypeComments[name]; hasComment && docComment != "" {
				lines := strings.Split(docComment, "\n")
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if line != "" {
						sb.WriteString(fmt.Sprintf("# %s\n", line))
					}
				}
			}
			// Add documentation link
			if result.GitHubBaseURL != "" {
				anchor := strings.ToLower(name)
				docLink := fmt.Sprintf("%s/docs/types/%s.md#%s", result.GitHubBaseURL, result.PackageName, anchor)
				sb.WriteString(fmt.Sprintf("# Documentation: %s\n", docLink))
			}
			sb.WriteString(typeCode)
			sb.WriteString("\n\n")
		}
	}

	// Generate dataclasses
	for _, name := range names {
		typeCode := result.Types[name]
		if strings.HasPrefix(typeCode, "@dataclass") {
			// Add doc comment if available
			if docComment, hasComment := result.TypeComments[name]; hasComment && docComment != "" {
				lines := strings.Split(docComment, "\n")
				sb.WriteString(fmt.Sprintf("# %s\n", strings.TrimSpace(lines[0])))
			}
			// Add documentation link
			if result.GitHubBaseURL != "" {
				anchor := strings.ToLower(name)
				docLink := fmt.Sprintf("%s/docs/types/%s.md#%s", result.GitHubBaseURL, result.PackageName, anchor)
				sb.WriteString(fmt.Sprintf("# Documentation: %s\n", docLink))
			}
			sb.WriteString(typeCode)
			sb.WriteString("\n\n")
		}
	}

	// Generate array exports (slice literals)
	if len(result.Arrays) > 0 {
		arrayNames := make([]string, 0, len(result.Arrays))
		for name := range result.Arrays {
			arrayNames = append(arrayNames, name)
		}
		sort.Strings(arrayNames)

		for _, name := range arrayNames {
			elements := result.Arrays[name]

			// Python constant naming: SCREAMING_SNAKE_CASE
			pyName := strings.ToUpper(util.ToSnakeCase(name))

			// Check if all elements are const references
			allConsts := true
			for _, elem := range elements {
				if !typegen.IsConstReference(elem, result.Consts) {
					allConsts = false
					break
				}
			}

			if allConsts {
				// Use const references
				pyElements := make([]string, len(elements))
				for i, elem := range elements {
					pyElements[i] = strings.ToUpper(util.ToSnakeCase(elem))
				}
				sb.WriteString(fmt.Sprintf("%s: tuple[str, ...] = (%s,)\n",
					pyName, strings.Join(pyElements, ", ")))
			} else {
				// Use string literals
				pyElements := make([]string, len(elements))
				for i, elem := range elements {
					if typegen.IsConstReference(elem, result.Consts) {
						pyElements[i] = strings.ToUpper(util.ToSnakeCase(elem))
					} else {
						pyElements[i] = fmt.Sprintf("\"%s\"", elem)
					}
				}
				sb.WriteString(fmt.Sprintf("%s: tuple[str, ...] = (%s,)\n",
					pyName, strings.Join(pyElements, ", ")))
			}
		}
		sb.WriteString("\n")
	}

	// Generate map exports (map literals)
	if len(result.Maps) > 0 {
		mapNames := make([]string, 0, len(result.Maps))
		for name := range result.Maps {
			mapNames = append(mapNames, name)
		}
		sort.Strings(mapNames)

		for _, name := range mapNames {
			mapData := result.Maps[name]
			pyName := strings.ToUpper(util.ToSnakeCase(name))

			sb.WriteString(fmt.Sprintf("%s: dict[str, str] = {\n", pyName))

			// Sort map keys for deterministic output
			keys := make([]string, 0, len(mapData))
			for k := range mapData {
				keys = append(keys, k)
			}
			sort.Strings(keys)

			for _, key := range keys {
				value := mapData[key]
				keyStr := formatMapKey(key, result.Consts)
				valueStr := formatMapValue(value, result.Consts)
				sb.WriteString(fmt.Sprintf("    %s: %s,\n", keyStr, valueStr))
			}

			sb.WriteString("}\n\n")
		}
	}

	// Generate __all__ export list
	sb.WriteString("__all__ = [\n")

	// Add const names
	constNames := make([]string, 0, len(result.Consts))
	for name := range result.Consts {
		constNames = append(constNames, name)
	}
	sort.Strings(constNames)
	for _, name := range constNames {
		pyName := strings.ToUpper(util.ToSnakeCase(name))
		sb.WriteString(fmt.Sprintf("    \"%s\",\n", pyName))
	}

	// Add type names
	for _, name := range names {
		sb.WriteString(fmt.Sprintf("    \"%s\",\n", name))
	}

	// Add array names
	arrayNames := make([]string, 0, len(result.Arrays))
	for name := range result.Arrays {
		arrayNames = append(arrayNames, name)
	}
	sort.Strings(arrayNames)
	for _, name := range arrayNames {
		pyName := strings.ToUpper(util.ToSnakeCase(name))
		sb.WriteString(fmt.Sprintf("    \"%s\",\n", pyName))
	}

	// Add map names
	mapNames := make([]string, 0, len(result.Maps))
	for name := range result.Maps {
		mapNames = append(mapNames, name)
	}
	sort.Strings(mapNames)
	for _, name := range mapNames {
		pyName := strings.ToUpper(util.ToSnakeCase(name))
		sb.WriteString(fmt.Sprintf("    \"%s\",\n", pyName))
	}

	sb.WriteString("]\n")

	return sb.String()
}

// toPyConstName converts an identifier to Python constant naming (SCREAMING_SNAKE_CASE)
func toPyConstName(s string) string {
	return strings.ToUpper(util.ToSnakeCase(s))
}

// formatMapKey formats a map key for Python output
func formatMapKey(key string, consts map[string]string) string {
	return typegen.FormatMapEntry(key, consts, toPyConstName)
}

// formatMapValue formats a map value for Python output
func formatMapValue(value string, consts map[string]string) string {
	return typegen.FormatMapEntry(value, consts, toPyConstName)
}
