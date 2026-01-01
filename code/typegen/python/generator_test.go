package python

import (
	"go/ast"
	"strings"
	"testing"

	"github.com/teranos/QNTX/code/typegen"
)

func TestParseFieldTags_JSONOnly(t *testing.T) {
	tag := createTag(`json:"field_name,omitempty"`)
	info := ParseFieldTags(tag)

	if info.JSONName != "field_name" {
		t.Errorf("Expected JSONName 'field_name', got '%s'", info.JSONName)
	}
	if !info.Omitempty {
		t.Error("Expected Omitempty to be true")
	}
	if info.PyType != "" {
		t.Errorf("Expected empty PyType, got '%s'", info.PyType)
	}
}

func TestParseFieldTags_PyTypeOverride(t *testing.T) {
	tag := createTag(`json:"field" pytype:"CustomType"`)
	info := ParseFieldTags(tag)

	if info.JSONName != "field" {
		t.Errorf("Expected JSONName 'field', got '%s'", info.JSONName)
	}
	if info.PyType != "CustomType" {
		t.Errorf("Expected PyType 'CustomType', got '%s'", info.PyType)
	}
}

func TestParseFieldTags_PyTypeWithUnion(t *testing.T) {
	tag := createTag(`json:"nullable" pytype:"str | None"`)
	info := ParseFieldTags(tag)

	if info.PyType != "str | None" {
		t.Errorf("Expected PyType 'str | None', got '%s'", info.PyType)
	}
}

func TestParseFieldTags_PyTypeOptional(t *testing.T) {
	tag := createTag(`json:"opt" pytype:"int,optional"`)
	info := ParseFieldTags(tag)

	if info.PyType != "int" {
		t.Errorf("Expected PyType 'int', got '%s'", info.PyType)
	}
	if !info.PyOptional {
		t.Error("Expected PyOptional to be true")
	}
}

func TestParseFieldTags_PyTypeSkip(t *testing.T) {
	tag := createTag(`json:"field" pytype:"-"`)
	info := ParseFieldTags(tag)

	if !info.Skip {
		t.Error("Expected Skip to be true for pytype:\"-\"")
	}
}

func TestParseFieldTags_JSONSkip(t *testing.T) {
	tag := createTag(`json:"-"`)
	info := ParseFieldTags(tag)

	if !info.Skip {
		t.Error("Expected Skip to be true for json:\"-\"")
	}
}

func TestParseFieldTags_NilTag(t *testing.T) {
	info := ParseFieldTags(nil)

	if info.JSONName != "" || info.PyType != "" || info.Skip {
		t.Error("Expected empty FieldTagInfo for nil tag")
	}
}

// createTag creates a mock ast.BasicLit for testing tag parsing
func createTag(tagValue string) *ast.BasicLit {
	return &ast.BasicLit{
		Value: "`" + tagValue + "`",
	}
}

// =============================================================================
// GenerateDataclass tests
// =============================================================================

func TestGenerateDataclass_Basic(t *testing.T) {
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

	result := GenerateDataclass("Person", structType)

	if !contains(result, "@dataclass") {
		t.Error("Expected @dataclass decorator")
	}
	if !contains(result, "class Person:") {
		t.Error("Expected class declaration")
	}
	if !contains(result, "name: str") {
		t.Error("Expected name field")
	}
	if !contains(result, "age: int") {
		t.Error("Expected age field")
	}
}

func TestGenerateDataclass_OptionalFields(t *testing.T) {
	structType := &ast.StructType{
		Fields: &ast.FieldList{
			List: []*ast.Field{
				{
					Names: []*ast.Ident{{Name: "Required"}},
					Type:  &ast.Ident{Name: "string"},
					Tag:   createTag(`json:"required"`),
				},
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

	result := GenerateDataclass("Contact", structType)

	// Required fields should come first
	reqIdx := indexOf(result, "required: str")
	emailIdx := indexOf(result, "email: str | None = None")
	phoneIdx := indexOf(result, "phone: str | None = None")

	if reqIdx == -1 {
		t.Error("Expected required field")
	}
	if emailIdx == -1 {
		t.Error("Expected optional email field with None default")
	}
	if phoneIdx == -1 {
		t.Error("Expected optional phone field with None union and default")
	}

	// Required should come before optional
	if reqIdx > emailIdx || reqIdx > phoneIdx {
		t.Error("Required fields should come before optional fields")
	}
}

func TestGenerateDataclass_ArrayFields(t *testing.T) {
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

	result := GenerateDataclass("Tagged", structType)

	if !contains(result, "tags: list[str]") {
		t.Error("Expected array field with list[str] type")
	}
}

func TestGenerateDataclass_SkipFields(t *testing.T) {
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

	result := GenerateDataclass("Mixed", structType)

	if !contains(result, "public: str") {
		t.Error("Expected public field")
	}
	if contains(result, "Internal") || contains(result, "internal") {
		t.Error("Should skip json:\"-\" field")
	}
	if contains(result, "private") {
		t.Error("Should skip unexported field")
	}
}

func TestGenerateDataclass_PyTypeOverride(t *testing.T) {
	structType := &ast.StructType{
		Fields: &ast.FieldList{
			List: []*ast.Field{
				{
					Names: []*ast.Ident{{Name: "Custom"}},
					Type:  &ast.Ident{Name: "interface{}"},
					Tag:   createTag(`json:"custom" pytype:"CustomType"`),
				},
			},
		},
	}

	result := GenerateDataclass("WithCustom", structType)

	if !contains(result, "custom: CustomType") {
		t.Error("Expected pytype override")
	}
}

func TestGenerateDataclass_EmptyStruct(t *testing.T) {
	structType := &ast.StructType{
		Fields: &ast.FieldList{
			List: []*ast.Field{},
		},
	}

	result := GenerateDataclass("Empty", structType)

	if !contains(result, "pass") {
		t.Error("Expected pass for empty dataclass")
	}
}

func TestGenerateDataclass_PythonKeywords(t *testing.T) {
	structType := &ast.StructType{
		Fields: &ast.FieldList{
			List: []*ast.Field{
				{
					Names: []*ast.Ident{{Name: "Type"}},
					Type:  &ast.Ident{Name: "string"},
					Tag:   createTag(`json:"type"`),
				},
				{
					Names: []*ast.Ident{{Name: "Class"}},
					Type:  &ast.Ident{Name: "string"},
					Tag:   createTag(`json:"class"`),
				},
			},
		},
	}

	result := GenerateDataclass("WithKeywords", structType)

	if !contains(result, "type_: str") {
		t.Error("Expected type field with underscore suffix")
	}
	if !contains(result, "class_: str") {
		t.Error("Expected class field with underscore suffix")
	}
}

// =============================================================================
// GenerateEnum tests
// =============================================================================

func TestGenerateEnum(t *testing.T) {
	values := []string{"active", "paused", "completed"}
	result := GenerateEnum("Status", values)

	// Values are sorted alphabetically for deterministic output
	if !contains(result, "class Status(str, Enum):") {
		t.Error("Expected class declaration with str, Enum")
	}
	if !contains(result, "ACTIVE = \"active\"") {
		t.Error("Expected ACTIVE enum member")
	}
	if !contains(result, "COMPLETED = \"completed\"") {
		t.Error("Expected COMPLETED enum member")
	}
	if !contains(result, "PAUSED = \"paused\"") {
		t.Error("Expected PAUSED enum member")
	}
}

func TestGenerateEnum_Single(t *testing.T) {
	values := []string{"only"}
	result := GenerateEnum("Single", values)

	if !contains(result, "class Single(str, Enum):") {
		t.Error("Expected class declaration")
	}
	if !contains(result, "ONLY = \"only\"") {
		t.Error("Expected ONLY enum member")
	}
}

func TestGenerateEnum_SnakeCase(t *testing.T) {
	values := []string{"ai_error", "network_error"}
	result := GenerateEnum("ErrorCode", values)

	if !contains(result, "AI_ERROR = \"ai_error\"") {
		t.Error("Expected AI_ERROR enum member")
	}
	if !contains(result, "NETWORK_ERROR = \"network_error\"") {
		t.Error("Expected NETWORK_ERROR enum member")
	}
}

// =============================================================================
// GenerateFile tests
// =============================================================================

func TestGenerateFile(t *testing.T) {
	gen := NewGenerator()
	result := &typegen.Result{
		Types: map[string]string{
			"TypeA": "@dataclass\nclass TypeA:\n    a: str",
			"TypeB": "@dataclass\nclass TypeB:\n    b: int",
		},
		PackageName: "test",
	}

	output := gen.GenerateFile(result)

	// Check header
	if !contains(output, "# Code generated") {
		t.Error("Expected generated code header")
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

	// Check imports
	if !contains(output, "from __future__ import annotations") {
		t.Error("Expected __future__ import")
	}
	if !contains(output, "from dataclasses import dataclass") {
		t.Error("Expected dataclass import")
	}

	// Check types are included
	if !contains(output, "TypeA") {
		t.Error("Expected TypeA in output")
	}
	if !contains(output, "TypeB") {
		t.Error("Expected TypeB in output")
	}

	// Check __all__ export
	if !contains(output, "__all__ = [") {
		t.Error("Expected __all__ export list")
	}
}

func TestGenerateFile_WithConsts(t *testing.T) {
	gen := NewGenerator()
	result := &typegen.Result{
		Types:       map[string]string{},
		Consts:      map[string]string{"Pulse": "꩜"},
		PackageName: "test",
	}

	output := gen.GenerateFile(result)

	if !contains(output, `PULSE = "꩜"`) {
		t.Error("Expected const with SCREAMING_SNAKE_CASE name")
	}
}

func TestGenerateFile_DeterministicOrder(t *testing.T) {
	gen := NewGenerator()
	result := &typegen.Result{
		Types: map[string]string{
			"Zebra": "@dataclass\nclass Zebra:\n    z: str",
			"Alpha": "@dataclass\nclass Alpha:\n    a: str",
			"Beta":  "@dataclass\nclass Beta:\n    b: str",
		},
		PackageName: "test",
	}

	output := gen.GenerateFile(result)

	// Types should be sorted alphabetically
	alphaIdx := indexOf(output, "class Alpha")
	betaIdx := indexOf(output, "class Beta")
	zebraIdx := indexOf(output, "class Zebra")

	if alphaIdx == -1 || betaIdx == -1 || zebraIdx == -1 {
		t.Fatal("Not all types found in output")
	}

	if !(alphaIdx < betaIdx && betaIdx < zebraIdx) {
		t.Error("Types not in alphabetical order")
	}
}

// =============================================================================
// goTypeToPython tests
// =============================================================================

func TestGoTypeToPython_BasicTypes(t *testing.T) {
	tests := []struct {
		name     string
		expr     ast.Expr
		expected string
	}{
		{
			name:     "string",
			expr:     &ast.Ident{Name: "string"},
			expected: "str",
		},
		{
			name:     "int",
			expr:     &ast.Ident{Name: "int"},
			expected: "int",
		},
		{
			name:     "bool",
			expr:     &ast.Ident{Name: "bool"},
			expected: "bool",
		},
		{
			name:     "float64",
			expr:     &ast.Ident{Name: "float64"},
			expected: "float",
		},
		{
			name:     "uint32",
			expr:     &ast.Ident{Name: "uint32"},
			expected: "int",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := goTypeToPython(tt.expr)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestGoTypeToPython_QualifiedTypes(t *testing.T) {
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
			expected: "str",
		},
		{
			name: "time.Duration",
			expr: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "time"},
				Sel: &ast.Ident{Name: "Duration"},
			},
			expected: "int",
		},
		{
			name: "json.RawMessage",
			expr: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "json"},
				Sel: &ast.Ident{Name: "RawMessage"},
			},
			expected: "Any",
		},
		{
			name: "sql.NullString",
			expr: &ast.SelectorExpr{
				X:   &ast.Ident{Name: "sql"},
				Sel: &ast.Ident{Name: "NullString"},
			},
			expected: "str | None",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := goTypeToPython(tt.expr)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestGoTypeToPython_TypeReference(t *testing.T) {
	expr := &ast.Ident{Name: "CustomType"}

	result := goTypeToPython(expr)
	if result != "CustomType" {
		t.Errorf("Expected 'CustomType', got %q", result)
	}
}

func TestGoTypeToPython_PointerType(t *testing.T) {
	expr := &ast.StarExpr{
		X: &ast.Ident{Name: "string"},
	}

	result := goTypeToPython(expr)
	if result != "str" {
		t.Errorf("Expected 'str', got %q", result)
	}
}

func TestGoTypeToPython_ArrayType(t *testing.T) {
	expr := &ast.ArrayType{
		Elt: &ast.Ident{Name: "string"},
	}

	result := goTypeToPython(expr)
	if result != "list[str]" {
		t.Errorf("Expected 'list[str]', got %q", result)
	}
}

func TestGoTypeToPython_MapStringInterface(t *testing.T) {
	expr := &ast.MapType{
		Key: &ast.Ident{Name: "string"},
		Value: &ast.InterfaceType{
			Methods: &ast.FieldList{},
		},
	}

	result := goTypeToPython(expr)
	if result != "dict[str, Any]" {
		t.Errorf("Expected 'dict[str, Any]', got %q", result)
	}
}

func TestGoTypeToPython_MapStringString(t *testing.T) {
	expr := &ast.MapType{
		Key:   &ast.Ident{Name: "string"},
		Value: &ast.Ident{Name: "string"},
	}

	result := goTypeToPython(expr)
	if result != "dict[str, str]" {
		t.Errorf("Expected 'dict[str, str]', got %q", result)
	}
}

func TestGoTypeToPython_InterfaceType(t *testing.T) {
	expr := &ast.InterfaceType{
		Methods: &ast.FieldList{},
	}

	result := goTypeToPython(expr)
	if result != "Any" {
		t.Errorf("Expected 'Any', got %q", result)
	}
}

func TestGoTypeToPython_UnknownType(t *testing.T) {
	expr := &ast.ChanType{
		Value: &ast.Ident{Name: "string"},
	}

	result := goTypeToPython(expr)
	if result != "Any" {
		t.Errorf("Expected 'Any', got %q", result)
	}
}

func TestGoTypeToPython_NestedArray(t *testing.T) {
	expr := &ast.ArrayType{
		Elt: &ast.ArrayType{
			Elt: &ast.Ident{Name: "string"},
		},
	}

	result := goTypeToPython(expr)
	if result != "list[list[str]]" {
		t.Errorf("Expected 'list[list[str]]', got %q", result)
	}
}

// =============================================================================
// Generator interface tests
// =============================================================================

func TestGenerator_Language(t *testing.T) {
	gen := NewGenerator()
	if gen.Language() != "python" {
		t.Errorf("Expected 'python', got '%s'", gen.Language())
	}
}

func TestGenerator_FileExtension(t *testing.T) {
	gen := NewGenerator()
	if gen.FileExtension() != "py" {
		t.Errorf("Expected 'py', got '%s'", gen.FileExtension())
	}
}

// =============================================================================
// toPythonIdent tests
// =============================================================================

func TestToPythonIdent_Keywords(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"type", "type_"},
		{"class", "class_"},
		{"async", "async_"},
		{"import", "import_"},
		{"from", "from_"},
		{"def", "def_"},
		{"return", "return_"},
		{"name", "name"}, // not a keyword
		{"value", "value"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := toPythonIdent(tt.input)
			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// =============================================================================
// Helper functions
// =============================================================================

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func indexOf(s, substr string) int {
	return strings.Index(s, substr)
}
