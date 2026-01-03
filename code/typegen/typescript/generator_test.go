package typescript

import (
	"go/ast"
	"testing"

	"github.com/teranos/QNTX/code/typegen"
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
	if info.CustomType != "" {
		t.Errorf("Expected empty CustomType, got '%s'", info.CustomType)
	}
}

func TestParseFieldTags_TSTypeOverride(t *testing.T) {
	// Test tstype override
	tag := createTag(`json:"field" tstype:"CustomType"`)
	info := ParseFieldTags(tag)

	if info.JSONName != "field" {
		t.Errorf("Expected JSONName 'field', got '%s'", info.JSONName)
	}
	if info.CustomType != "CustomType" {
		t.Errorf("Expected CustomType 'CustomType', got '%s'", info.CustomType)
	}
}

func TestParseFieldTags_TSTypeWithUnion(t *testing.T) {
	// Test tstype with union type (common use case)
	tag := createTag(`json:"nullable" tstype:"string | null"`)
	info := ParseFieldTags(tag)

	if info.CustomType != "string | null" {
		t.Errorf("Expected CustomType 'string | null', got '%s'", info.CustomType)
	}
}

func TestParseFieldTags_TSTypeOptional(t *testing.T) {
	// Test tstype with optional modifier
	tag := createTag(`json:"opt" tstype:"number,optional"`)
	info := ParseFieldTags(tag)

	if info.CustomType != "number" {
		t.Errorf("Expected CustomType 'number', got '%s'", info.CustomType)
	}
	if !info.CustomOptional {
		t.Error("Expected CustomOptional to be true")
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

	if info.JSONName != "" || info.CustomType != "" || info.Skip || info.Readonly {
		t.Error("Expected empty FieldTagInfo for nil tag")
	}
}

func TestParseFieldTags_Readonly(t *testing.T) {
	// Test readonly tag
	tag := createTag(`json:"field" readonly:"true"`)
	info := ParseFieldTags(tag)

	if !info.Readonly {
		t.Error("Expected Readonly to be true")
	}
}

func TestParseFieldTags_ReadonlyShort(t *testing.T) {
	// Test readonly tag without value
	tag := createTag(`json:"field" readonly:""`)
	info := ParseFieldTags(tag)

	if !info.Readonly {
		t.Error("Expected Readonly to be true for empty readonly tag")
	}
}

func TestParseFieldTags_ReadonlyFalse(t *testing.T) {
	// Test readonly:"false"
	tag := createTag(`json:"field" readonly:"false"`)
	info := ParseFieldTags(tag)

	if info.Readonly {
		t.Error("Expected Readonly to be false")
	}
}

// createTag creates a mock ast.BasicLit for testing tag parsing
func createTag(tagValue string) *ast.BasicLit {
	return &ast.BasicLit{
		Value: "`" + tagValue + "`",
	}
}

// =============================================================================
// GenerateInterface tests
// =============================================================================

func TestGenerateInterface_Basic(t *testing.T) {
	// Simple struct with basic fields
	structType := &ast.StructType{
		Fields: &ast.FieldList{
			List: []*ast.Field{
				{
					Names: []*ast.Ident{{Name: "Name"}},
					Type:  &ast.Ident{Name: "string"},
					Tag:   createTag(`json:"name"`),
				},
				{
					Names: []*ast.Ident{{Name: "Age"}},
					Type:  &ast.Ident{Name: "int"},
					Tag:   createTag(`json:"age"`),
				},
			},
		},
	}

	result := GenerateInterface("Person", structType)

	if !contains(result, "export interface Person {") {
		t.Error("Expected interface declaration")
	}
	if !contains(result, "name: string;") {
		t.Error("Expected name field")
	}
	if !contains(result, "age: number;") {
		t.Error("Expected age field")
	}
}

func TestGenerateInterface_OptionalFields(t *testing.T) {
	// Struct with optional fields (omitempty and pointer)
	structType := &ast.StructType{
		Fields: &ast.FieldList{
			List: []*ast.Field{
				{
					Names: []*ast.Ident{{Name: "Email"}},
					Type:  &ast.Ident{Name: "string"},
					Tag:   createTag(`json:"email,omitempty"`),
				},
				{
					Names: []*ast.Ident{{Name: "Phone"}},
					Type:  &ast.StarExpr{X: &ast.Ident{Name: "string"}},
					Tag:   createTag(`json:"phone"`),
				},
			},
		},
	}

	result := GenerateInterface("Contact", structType)

	if !contains(result, "email?: string;") {
		t.Error("Expected optional email field")
	}
	if !contains(result, "phone?: string | null;") {
		t.Error("Expected optional phone field with null union")
	}
}

func TestGenerateInterface_ArrayFields(t *testing.T) {
	// Struct with array/slice field
	structType := &ast.StructType{
		Fields: &ast.FieldList{
			List: []*ast.Field{
				{
					Names: []*ast.Ident{{Name: "Tags"}},
					Type:  &ast.ArrayType{Elt: &ast.Ident{Name: "string"}},
					Tag:   createTag(`json:"tags"`),
				},
			},
		},
	}

	result := GenerateInterface("Tagged", structType)

	if !contains(result, "tags: string[];") {
		t.Error("Expected array field")
	}
}

func TestGenerateInterface_SkipFields(t *testing.T) {
	// Struct with fields that should be skipped
	structType := &ast.StructType{
		Fields: &ast.FieldList{
			List: []*ast.Field{
				{
					Names: []*ast.Ident{{Name: "Public"}},
					Type:  &ast.Ident{Name: "string"},
					Tag:   createTag(`json:"public"`),
				},
				{
					Names: []*ast.Ident{{Name: "Internal"}},
					Type:  &ast.Ident{Name: "string"},
					Tag:   createTag(`json:"-"`),
				},
				{
					Names: []*ast.Ident{{Name: "private"}},
					Type:  &ast.Ident{Name: "string"},
					Tag:   createTag(`json:"private"`),
				},
			},
		},
	}

	result := GenerateInterface("Mixed", structType)

	if !contains(result, "public: string;") {
		t.Error("Expected public field")
	}
	if contains(result, "Internal") || contains(result, "internal") {
		t.Error("Should skip json:\"-\" field")
	}
	if contains(result, "private") {
		t.Error("Should skip unexported field")
	}
}

func TestGenerateInterface_TSTypeOverride(t *testing.T) {
	// Struct with tstype override
	structType := &ast.StructType{
		Fields: &ast.FieldList{
			List: []*ast.Field{
				{
					Names: []*ast.Ident{{Name: "Custom"}},
					Type:  &ast.Ident{Name: "interface{}"},
					Tag:   createTag(`json:"custom" tstype:"CustomType"`),
				},
			},
		},
	}

	result := GenerateInterface("WithCustom", structType)

	if !contains(result, "custom: CustomType;") {
		t.Error("Expected tstype override")
	}
}

func TestGenerateInterface_Readonly(t *testing.T) {
	// Struct with readonly field
	structType := &ast.StructType{
		Fields: &ast.FieldList{
			List: []*ast.Field{
				{
					Names: []*ast.Ident{{Name: "ID"}},
					Type:  &ast.Ident{Name: "string"},
					Tag:   createTag(`json:"id" readonly:"true"`),
				},
			},
		},
	}

	result := GenerateInterface("WithReadonly", structType)

	if !contains(result, "readonly id: string;") {
		t.Error("Expected readonly modifier")
	}
}

func TestGenerateInterface_Comments(t *testing.T) {
	// Struct with field comments
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
				{
					Names: []*ast.Ident{{Name: "Email"}},
					Type:  &ast.Ident{Name: "string"},
					Tag:   createTag(`json:"email"`),
					Comment: &ast.CommentGroup{
						List: []*ast.Comment{
							{Text: "// Primary email address"},
						},
					},
				},
			},
		},
	}

	result := GenerateInterface("WithComments", structType)

	// Check for multi-line JSDoc format (new format with validation support)
	if !contains(result, "/**\n   * User's full name\n   */") {
		t.Error("Expected Doc comment for name field")
	}
	if !contains(result, "/**\n   * Primary email address\n   */") {
		t.Error("Expected Comment for email field")
	}
}

func TestGenerateInterface_MultilineComment(t *testing.T) {
	// Struct with multiline comment
	structType := &ast.StructType{
		Fields: &ast.FieldList{
			List: []*ast.Field{
				{
					Names: []*ast.Ident{{Name: "Description"}},
					Type:  &ast.Ident{Name: "string"},
					Tag:   createTag(`json:"description"`),
					Doc: &ast.CommentGroup{
						List: []*ast.Comment{
							{Text: "// A detailed description"},
							{Text: "// that spans multiple lines"},
						},
					},
				},
			},
		},
	}

	result := GenerateInterface("WithMultiline", structType)

	// Check for multi-line JSDoc format
	if !contains(result, "/**\n   * A detailed description that spans multiple lines\n   */") {
		t.Error("Expected multiline comment joined")
	}
}

// =============================================================================
// GenerateUnionType tests
// =============================================================================

func TestGenerateUnionType(t *testing.T) {
	values := []string{"active", "paused", "completed"}
	result := GenerateUnionType("Status", values)

	// Values are sorted alphabetically for deterministic output
	expected := "export type Status = 'active' | 'completed' | 'paused';"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

func TestGenerateUnionType_Single(t *testing.T) {
	values := []string{"only"}
	result := GenerateUnionType("Single", values)

	expected := "export type Single = 'only';"
	if result != expected {
		t.Errorf("Expected %q, got %q", expected, result)
	}
}

// =============================================================================
// GenerateFile tests
// =============================================================================

func TestGenerateFile(t *testing.T) {
	gen := NewGenerator()
	result := &typegen.Result{
		Types: map[string]string{
			"TypeA": "export interface TypeA { a: string; }",
			"TypeB": "export interface TypeB { b: number; }",
		},
		PackageName: "test",
	}

	output := gen.GenerateFile(result)

	// Check header
	if !contains(output, "/* eslint-disable */") {
		t.Error("Expected eslint disable comment")
	}
	if !contains(output, "DO NOT EDIT") {
		t.Error("Expected DO NOT EDIT warning")
	}
	if !contains(output, "make types") {
		t.Error("Expected regeneration instructions")
	}
	if !contains(output, "Source package: test") {
		t.Error("Expected package name in header")
	}

	// Check types are included
	if !contains(output, "TypeA") {
		t.Error("Expected TypeA in output")
	}
	if !contains(output, "TypeB") {
		t.Error("Expected TypeB in output")
	}
}

func TestGenerateFile_DeterministicOrder(t *testing.T) {
	gen := NewGenerator()
	result := &typegen.Result{
		Types: map[string]string{
			"Zebra": "export type Zebra = string;",
			"Alpha": "export type Alpha = string;",
			"Beta":  "export type Beta = string;",
		},
		PackageName: "test",
	}

	output := gen.GenerateFile(result)

	// Types should be sorted alphabetically
	alphaIdx := indexOf(output, "Alpha")
	betaIdx := indexOf(output, "Beta")
	zebraIdx := indexOf(output, "Zebra")

	if alphaIdx == -1 || betaIdx == -1 || zebraIdx == -1 {
		t.Fatal("Not all types found in output")
	}

	if !(alphaIdx < betaIdx && betaIdx < zebraIdx) {
		t.Error("Types not in alphabetical order")
	}
}

// =============================================================================
// goTypeToTS tests
// =============================================================================

func TestGoTypeToTS_BasicTypes(t *testing.T) {
	tests := []struct {
		name     string
		expr     ast.Expr
		expected string
	}{
		{
			name:     "string",
			expr:     &ast.Ident{Name: "string"},
			expected: "string",
		},
		{
			name:     "int",
			expr:     &ast.Ident{Name: "int"},
			expected: "number",
		},
		{
			name:     "bool",
			expr:     &ast.Ident{Name: "bool"},
			expected: "boolean",
		},
		{
			name:     "float64",
			expr:     &ast.Ident{Name: "float64"},
			expected: "number",
		},
		{
			name:     "uint32",
			expr:     &ast.Ident{Name: "uint32"},
			expected: "number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := goTypeToTS(tt.expr)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestGoTypeToTS_QualifiedTypes(t *testing.T) {
	tests := []struct {
		name     string
		expr     ast.Expr
		expected string
	}{
		{
			name: "time.Time",
			expr: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "time"},
				Sel: &ast.Ident{Name: "Time"},
			},
			expected: "string",
		},
		{
			name: "time.Duration",
			expr: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "time"},
				Sel: &ast.Ident{Name: "Duration"},
			},
			expected: "number",
		},
		{
			name: "json.RawMessage",
			expr: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "json"},
				Sel: &ast.Ident{Name: "RawMessage"},
			},
			expected: "unknown",
		},
		{
			name: "sql.NullString",
			expr: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "sql"},
				Sel: &ast.Ident{Name: "NullString"},
			},
			expected: "string | null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := goTypeToTS(tt.expr)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestGoTypeToTS_UnknownQualifiedType(t *testing.T) {
	// Unknown package.Type should return just the type name
	expr := &ast.SelectorExpr{
		X:   &ast.Ident{Name: "custom"},
		Sel: &ast.Ident{Name: "CustomType"},
	}

	result := goTypeToTS(expr)
	if result != "CustomType" {
		t.Errorf("Expected 'CustomType', got %q", result)
	}
}

func TestGoTypeToTS_SelectorExpr_NonIdent(t *testing.T) {
	// SelectorExpr where X is not an Ident should return "unknown"
	expr := &ast.SelectorExpr{
		X: &ast.StarExpr{
			X: &ast.Ident{Name: "SomeType"},
		},
		Sel: &ast.Ident{Name: "Field"},
	}

	result := goTypeToTS(expr)
	if result != "unknown" {
		t.Errorf("Expected 'unknown', got %q", result)
	}
}

func TestGoTypeToTS_TypeReference(t *testing.T) {
	// Reference to another type in the same package
	expr := &ast.Ident{Name: "CustomType"}

	result := goTypeToTS(expr)
	if result != "CustomType" {
		t.Errorf("Expected 'CustomType', got %q", result)
	}
}

func TestGoTypeToTS_PointerType(t *testing.T) {
	// *string should return "string"
	expr := &ast.StarExpr{
		X: &ast.Ident{Name: "string"},
	}

	result := goTypeToTS(expr)
	if result != "string" {
		t.Errorf("Expected 'string', got %q", result)
	}
}

func TestGoTypeToTS_PointerToCustomType(t *testing.T) {
	// *CustomType should return "CustomType"
	expr := &ast.StarExpr{
		X: &ast.Ident{Name: "CustomType"},
	}

	result := goTypeToTS(expr)
	if result != "CustomType" {
		t.Errorf("Expected 'CustomType', got %q", result)
	}
}

func TestGoTypeToTS_ArrayType(t *testing.T) {
	// []string should return "string[]"
	expr := &ast.ArrayType{
		Elt: &ast.Ident{Name: "string"},
	}

	result := goTypeToTS(expr)
	if result != "string[]" {
		t.Errorf("Expected 'string[]', got %q", result)
	}
}

func TestGoTypeToTS_ArrayOfCustomType(t *testing.T) {
	// []CustomType should return "CustomType[]"
	expr := &ast.ArrayType{
		Elt: &ast.Ident{Name: "CustomType"},
	}

	result := goTypeToTS(expr)
	if result != "CustomType[]" {
		t.Errorf("Expected 'CustomType[]', got %q", result)
	}
}

func TestGoTypeToTS_MapStringInterface(t *testing.T) {
	// map[string]interface{} should return "Record<string, unknown>"
	expr := &ast.MapType{
		Key: &ast.Ident{Name: "string"},
		Value: &ast.InterfaceType{
			Methods: &ast.FieldList{},
		},
	}

	result := goTypeToTS(expr)
	if result != "Record<string, unknown>" {
		t.Errorf("Expected 'Record<string, unknown>', got %q", result)
	}
}

func TestGoTypeToTS_MapStringString(t *testing.T) {
	// map[string]string should return "Record<string, string>"
	expr := &ast.MapType{
		Key:   &ast.Ident{Name: "string"},
		Value: &ast.Ident{Name: "string"},
	}

	result := goTypeToTS(expr)
	if result != "Record<string, string>" {
		t.Errorf("Expected 'Record<string, string>', got %q", result)
	}
}

func TestGoTypeToTS_MapIntString(t *testing.T) {
	// map[int]string should return "Record<number, string>"
	expr := &ast.MapType{
		Key:   &ast.Ident{Name: "int"},
		Value: &ast.Ident{Name: "string"},
	}

	result := goTypeToTS(expr)
	if result != "Record<number, string>" {
		t.Errorf("Expected 'Record<number, string>', got %q", result)
	}
}

func TestGoTypeToTS_MapCustomTypes(t *testing.T) {
	// map[KeyType]ValueType should return "Record<KeyType, ValueType>"
	expr := &ast.MapType{
		Key:   &ast.Ident{Name: "KeyType"},
		Value: &ast.Ident{Name: "ValueType"},
	}

	result := goTypeToTS(expr)
	if result != "Record<KeyType, ValueType>" {
		t.Errorf("Expected 'Record<KeyType, ValueType>', got %q", result)
	}
}

func TestGoTypeToTS_InterfaceType(t *testing.T) {
	// interface{} should return "unknown"
	expr := &ast.InterfaceType{
		Methods: &ast.FieldList{},
	}

	result := goTypeToTS(expr)
	if result != "unknown" {
		t.Errorf("Expected 'unknown', got %q", result)
	}
}

func TestGoTypeToTS_UnknownType(t *testing.T) {
	// Unsupported AST node should return "unknown"
	expr := &ast.ChanType{
		Value: &ast.Ident{Name: "string"},
	}

	result := goTypeToTS(expr)
	if result != "unknown" {
		t.Errorf("Expected 'unknown', got %q", result)
	}
}

func TestGoTypeToTS_NestedArray(t *testing.T) {
	// [][]string should return "string[][]"
	expr := &ast.ArrayType{
		Elt: &ast.ArrayType{
			Elt: &ast.Ident{Name: "string"},
		},
	}

	result := goTypeToTS(expr)
	if result != "string[][]" {
		t.Errorf("Expected 'string[][]', got %q", result)
	}
}

func TestGoTypeToTS_ArrayOfPointers(t *testing.T) {
	// []*string should return "string[]"
	expr := &ast.ArrayType{
		Elt: &ast.StarExpr{
			X: &ast.Ident{Name: "string"},
		},
	}

	result := goTypeToTS(expr)
	if result != "string[]" {
		t.Errorf("Expected 'string[]', got %q", result)
	}
}

func TestGoTypeToTS_PointerToArray(t *testing.T) {
	// *[]string should return "string[]"
	expr := &ast.StarExpr{
		X: &ast.ArrayType{
			Elt: &ast.Ident{Name: "string"},
		},
	}

	result := goTypeToTS(expr)
	if result != "string[]" {
		t.Errorf("Expected 'string[]', got %q", result)
	}
}

// =============================================================================
// Generator interface tests
// =============================================================================

func TestGenerator_Language(t *testing.T) {
	gen := NewGenerator()
	if gen.Language() != "typescript" {
		t.Errorf("Expected 'typescript', got '%s'", gen.Language())
	}
}

func TestGenerator_FileExtension(t *testing.T) {
	gen := NewGenerator()
	if gen.FileExtension() != "ts" {
		t.Errorf("Expected 'ts', got '%s'", gen.FileExtension())
	}
}

// =============================================================================
// Helper functions
// =============================================================================

func contains(s, substr string) bool {
	return indexOf(s, substr) != -1
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
