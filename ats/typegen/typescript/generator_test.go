package typescript

import (
	"go/ast"
	"testing"
)

func TestParseFieldTags_JSONOnly(t *testing.T) {
	// Test basic json tag parsing
	tag := createTag(`json:"field_name,omitempty"`)
	info := ParseFieldTags(tag)

	if info.JSONName != "field_name" {
		t.Errorf("Expected JSONName 'field_name', got '%s'", info.JSONName)
	}
	if !info.Omitempty {
		t.Error("Expected Omitempty to be true")
	}
	if info.TSType != "" {
		t.Errorf("Expected empty TSType, got '%s'", info.TSType)
	}
}

func TestParseFieldTags_TSTypeOverride(t *testing.T) {
	// Test tstype override
	tag := createTag(`json:"field" tstype:"CustomType"`)
	info := ParseFieldTags(tag)

	if info.JSONName != "field" {
		t.Errorf("Expected JSONName 'field', got '%s'", info.JSONName)
	}
	if info.TSType != "CustomType" {
		t.Errorf("Expected TSType 'CustomType', got '%s'", info.TSType)
	}
}

func TestParseFieldTags_TSTypeWithUnion(t *testing.T) {
	// Test tstype with union type (common use case)
	tag := createTag(`json:"nullable" tstype:"string | null"`)
	info := ParseFieldTags(tag)

	if info.TSType != "string | null" {
		t.Errorf("Expected TSType 'string | null', got '%s'", info.TSType)
	}
}

func TestParseFieldTags_TSTypeOptional(t *testing.T) {
	// Test tstype with optional modifier
	tag := createTag(`json:"opt" tstype:"number,optional"`)
	info := ParseFieldTags(tag)

	if info.TSType != "number" {
		t.Errorf("Expected TSType 'number', got '%s'", info.TSType)
	}
	if !info.TSOptional {
		t.Error("Expected TSOptional to be true")
	}
}

func TestParseFieldTags_TSTypeSkip(t *testing.T) {
	// Test tstype:"-" to skip field
	tag := createTag(`json:"field" tstype:"-"`)
	info := ParseFieldTags(tag)

	if !info.Skip {
		t.Error("Expected Skip to be true for tstype:\"-\"")
	}
}

func TestParseFieldTags_JSONSkip(t *testing.T) {
	// Test json:"-" to skip field
	tag := createTag(`json:"-"`)
	info := ParseFieldTags(tag)

	if !info.Skip {
		t.Error("Expected Skip to be true for json:\"-\"")
	}
}

func TestParseFieldTags_NilTag(t *testing.T) {
	// Test nil tag (no struct tags)
	info := ParseFieldTags(nil)

	if info.JSONName != "" || info.TSType != "" || info.Skip {
		t.Error("Expected empty FieldTagInfo for nil tag")
	}
}

// createTag creates a mock ast.BasicLit for testing tag parsing
func createTag(tagValue string) *ast.BasicLit {
	return &ast.BasicLit{
		Value: "`" + tagValue + "`",
	}
}
