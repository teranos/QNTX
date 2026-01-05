package rust

import (
	"go/ast"
	"go/token"
	"strings"
	"testing"

	"github.com/teranos/QNTX/typegen/util"
)

// =============================================================================
// Test helpers
// =============================================================================

func createTag(tag string) *ast.BasicLit {
	return &ast.BasicLit{
		Kind:  token.STRING,
		Value: "`" + tag + "`",
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// =============================================================================
// ParseFieldTags tests
// =============================================================================

func TestParseFieldTags(t *testing.T) {
	tests := []struct {
		name        string
		tag         string
		expectedTag util.FieldTagInfo
	}{
		{
			name: "simple json tag",
			tag:  `json:"field_name"`,
			expectedTag: util.FieldTagInfo{
				JSONName:       "field_name",
				Omitempty:      false,
				Skip:           false,
				CustomType:     "",
				CustomOptional: false,
			},
		},
		{
			name: "json with omitempty",
			tag:  `json:"field_name,omitempty"`,
			expectedTag: util.FieldTagInfo{
				JSONName:       "field_name",
				Omitempty:      true,
				Skip:           false,
				CustomType:     "",
				CustomOptional: false,
			},
		},
		{
			name: "json skip",
			tag:  `json:"-"`,
			expectedTag: util.FieldTagInfo{
				JSONName:  "-",
				Omitempty: false,
				Skip:      true,
			},
		},
		{
			name: "rusttype override",
			tag:  `json:"data" rusttype:"Vec<u8>"`,
			expectedTag: util.FieldTagInfo{
				JSONName:       "data",
				Omitempty:      false,
				Skip:           false,
				CustomType:     "Vec<u8>",
				CustomOptional: false,
			},
		},
		{
			name: "rusttype with optional",
			tag:  `json:"name" rusttype:"String,optional"`,
			expectedTag: util.FieldTagInfo{
				JSONName:       "name",
				Omitempty:      false,
				Skip:           false,
				CustomType:     "String",
				CustomOptional: true,
			},
		},
		{
			name: "rusttype skip",
			tag:  `json:"field" rusttype:"-"`,
			expectedTag: util.FieldTagInfo{
				JSONName:  "field",
				Omitempty: false,
				Skip:      true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tag := createTag(tt.tag)
			result := ParseFieldTags(tag)

			if result.JSONName != tt.expectedTag.JSONName {
				t.Errorf("JSONName = %q, want %q", result.JSONName, tt.expectedTag.JSONName)
			}
			if result.Omitempty != tt.expectedTag.Omitempty {
				t.Errorf("Omitempty = %v, want %v", result.Omitempty, tt.expectedTag.Omitempty)
			}
			if result.Skip != tt.expectedTag.Skip {
				t.Errorf("Skip = %v, want %v", result.Skip, tt.expectedTag.Skip)
			}
			if result.CustomType != tt.expectedTag.CustomType {
				t.Errorf("CustomType = %q, want %q", result.CustomType, tt.expectedTag.CustomType)
			}
			if result.CustomOptional != tt.expectedTag.CustomOptional {
				t.Errorf("CustomOptional = %v, want %v", result.CustomOptional, tt.expectedTag.CustomOptional)
			}
		})
	}
}

// =============================================================================
// GenerateStruct tests
// =============================================================================

func TestGenerateStruct_SkipUnexported(t *testing.T) {
	structType := &ast.StructType{
		Fields: &ast.FieldList{
			List: []*ast.Field{
				{
					Names: []*ast.Ident{{Name: "PublicField"}},
					Type:  &ast.Ident{Name: "string"},
					Tag:   createTag(`json:"public"`),
				},
				{
					Names: []*ast.Ident{{Name: "privateField"}},
					Type:  &ast.Ident{Name: "string"},
					Tag:   createTag(`json:"private"`),
				},
			},
		},
	}

	result := GenerateStruct("Test", structType)

	// Field uses snake_case from Go field name
	if !contains(result, "pub public_field: String") {
		t.Error("Expected public_field")
	}
	if contains(result, "private") {
		t.Error("Should not contain private field")
	}
}

func TestGenerateStruct_OptionalFields(t *testing.T) {
	structType := &ast.StructType{
		Fields: &ast.FieldList{
			List: []*ast.Field{
				{
					Names: []*ast.Ident{{Name: "Required"}},
					Type:  &ast.Ident{Name: "string"},
					Tag:   createTag(`json:"required"`),
				},
				{
					Names: []*ast.Ident{{Name: "Optional"}},
					Type:  &ast.StarExpr{X: &ast.Ident{Name: "string"}},
					Tag:   createTag(`json:"optional,omitempty"`),
				},
			},
		},
	}

	result := GenerateStruct("Test", structType)

	if !contains(result, "pub required: String,") {
		t.Error("Expected required field as String")
	}
	if !contains(result, "pub optional: Option<String>,") {
		t.Error("Expected optional field as Option<String>")
	}
	if !contains(result, `#[serde(skip_serializing_if = "Option::is_none")]`) {
		t.Error("Expected skip_serializing_if for optional field")
	}
}

func TestGenerateStruct_Comments(t *testing.T) {
	structType := &ast.StructType{
		Fields: &ast.FieldList{
			List: []*ast.Field{
				{
					Names: []*ast.Ident{{Name: "Name"}},
					Type:  &ast.Ident{Name: "string"},
					Tag:   createTag(`json:"name"`),
					Doc: &ast.CommentGroup{
						List: []*ast.Comment{
							{Text: "// User's full name"},
						},
					},
				},
			},
		},
	}

	result := GenerateStruct("User", structType)

	if !contains(result, "/// User's full name") {
		t.Error("Expected doc comment")
	}
}

func TestGenerateStruct_ValidationMetadata(t *testing.T) {
	structType := &ast.StructType{
		Fields: &ast.FieldList{
			List: []*ast.Field{
				{
					Names: []*ast.Ident{{Name: "Items"}},
					Type:  &ast.ArrayType{Elt: &ast.Ident{Name: "string"}},
					Tag:   createTag(`json:"items" validate:"required,min=1"`),
				},
			},
		},
	}

	result := GenerateStruct("Test", structType)

	if !contains(result, "/// Validation: required") {
		t.Error("Expected required validation comment")
	}
	if !contains(result, "/// Validation: min items: 1") {
		t.Error("Expected min items validation comment (type-specific)")
	}
}

// =============================================================================
// GenerateEnum tests
// =============================================================================

func TestGenerateEnum(t *testing.T) {
	values := []string{"pending", "running", "completed"}

	result := GenerateEnum("Status", values)

	// Check derives
	if !contains(result, "#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]") {
		t.Error("Expected derive attributes")
	}

	// Check enum declaration
	if !contains(result, "pub enum Status {") {
		t.Error("Expected enum declaration")
	}

	// Check variants (should be sorted alphabetically)
	if !contains(result, `#[serde(rename = "completed")]`) {
		t.Error("Expected completed variant")
	}
	if !contains(result, "Completed,") {
		t.Error("Expected Completed variant in PascalCase")
	}

	if !contains(result, `#[serde(rename = "pending")]`) {
		t.Error("Expected pending variant")
	}
	if !contains(result, "Pending,") {
		t.Error("Expected Pending variant in PascalCase")
	}

	if !contains(result, `#[serde(rename = "running")]`) {
		t.Error("Expected running variant")
	}
	if !contains(result, "Running,") {
		t.Error("Expected Running variant in PascalCase")
	}
}

// =============================================================================
// Type mapping tests
// =============================================================================

func TestGoTypeToRust(t *testing.T) {
	tests := []struct {
		name     string
		goType   ast.Expr
		expected string
	}{
		{
			name:     "string",
			goType:   &ast.Ident{Name: "string"},
			expected: "String",
		},
		{
			name:     "int",
			goType:   &ast.Ident{Name: "int"},
			expected: "i64",
		},
		{
			name:     "bool",
			goType:   &ast.Ident{Name: "bool"},
			expected: "bool",
		},
		{
			name:     "slice of strings",
			goType:   &ast.ArrayType{Elt: &ast.Ident{Name: "string"}},
			expected: "Vec<String>",
		},
		{
			name: "time.Time",
			goType: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "time"},
				Sel: &ast.Ident{Name: "Time"},
			},
			expected: "String",
		},
		{
			name: "pointer to string",
			goType: &ast.StarExpr{
				X: &ast.Ident{Name: "string"},
			},
			expected: "String", // Pointer handled separately in field processing
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := goTypeToRust(tt.goType)
			if result != tt.expected {
				t.Errorf("goTypeToRust() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// =============================================================================
// Identifier conversion tests
// =============================================================================

func TestToRustIdent(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"normal", "normal"},
		{"type", "r#type"},   // Rust keyword
		{"match", "r#match"}, // Rust keyword
		{"async", "r#async"}, // Rust keyword
		{"as", "r#as"},       // Rust keyword
		{"self", "r#self"},   // Rust keyword
		{"not_keyword", "not_keyword"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := toRustIdent(tt.input)
			if result != tt.expected {
				t.Errorf("toRustIdent(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestToRustConstIdent(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"PascalCase", "PASCAL_CASE"},
		{"as", "r#AS"},     // Keyword in snake_case form
		{"type", "r#TYPE"}, // Keyword
		{"NotKeyword", "NOT_KEYWORD"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := toRustConstIdent(tt.input)
			if result != tt.expected {
				t.Errorf("toRustConstIdent(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
