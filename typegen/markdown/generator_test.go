package markdown

import (
	"fmt"
	"go/ast"
	"strings"
	"testing"

	"github.com/teranos/QNTX/typegen"
)

func TestGenerateUnionType_SingleChar(t *testing.T) {
	gen := NewGenerator()
	values := []string{"A", "B", "C"}
	result := gen.GenerateUnionType("Letter", values)

	// Should not panic
	if result == "" {
		t.Fatal("Expected non-empty result")
	}

	// Should contain the type declaration
	if !strings.Contains(result, "type Letter string") {
		t.Error("Expected type declaration")
	}

	// Should contain all const values
	for _, v := range values {
		if !strings.Contains(result, fmt.Sprintf(`"%s"`, v)) {
			t.Errorf("Expected const value %q in output", v)
		}
	}
}

func TestGenerateUnionType_EmptyValue(t *testing.T) {
	gen := NewGenerator()
	values := []string{""}
	result := gen.GenerateUnionType("Empty", values)

	// Should handle empty string without panic
	if result == "" {
		t.Fatal("Expected non-empty result")
	}
}

func TestGenerateUnionType_MixedLength(t *testing.T) {
	gen := NewGenerator()
	values := []string{"a", "bb", "ccc"}
	result := gen.GenerateUnionType("Mixed", values)

	// Should contain all values
	if !strings.Contains(result, `"a"`) {
		t.Error("Expected single-char value")
	}
	if !strings.Contains(result, `"bb"`) {
		t.Error("Expected two-char value")
	}
	if !strings.Contains(result, `"ccc"`) {
		t.Error("Expected three-char value")
	}

	// Should contain Go const declarations
	if !strings.Contains(result, "MixedA Mixed = \"a\"") {
		t.Error("Expected proper const name for single-char")
	}
	if !strings.Contains(result, "MixedBb Mixed = \"bb\"") {
		t.Error("Expected proper const name for two-char")
	}
	if !strings.Contains(result, "MixedCcc Mixed = \"ccc\"") {
		t.Error("Expected proper const name for three-char")
	}
}

func TestGenerateInterface_EmptyStruct(t *testing.T) {
	gen := NewGenerator()
	structType := &ast.StructType{
		Fields: &ast.FieldList{List: []*ast.Field{}},
	}
	result := gen.GenerateInterface("Empty", structType)

	// Should handle empty struct gracefully
	if result == "" {
		t.Fatal("Expected non-empty result")
	}

	// Should contain table structure
	if !strings.Contains(result, "<table>") {
		t.Error("Expected HTML table")
	}
	if !strings.Contains(result, "## Empty") {
		t.Error("Expected heading")
	}
}

func TestGenerateInterface_BasicFields(t *testing.T) {
	gen := NewGenerator()

	// Create a simple struct: type Example struct { Name string }
	structType := &ast.StructType{
		Fields: &ast.FieldList{
			List: []*ast.Field{
				{
					Names: []*ast.Ident{ast.NewIdent("Name")},
					Type:  ast.NewIdent("string"),
					Tag:   &ast.BasicLit{Value: "`json:\"name\"`"},
				},
			},
		},
	}

	result := gen.GenerateInterface("Example", structType)

	// Should contain Go struct
	if !strings.Contains(result, "type Example struct {") {
		t.Error("Expected Go struct declaration")
	}
	if !strings.Contains(result, "Name string") {
		t.Error("Expected field in Go struct")
	}
	if !strings.Contains(result, "`json:\"name\"`") {
		t.Error("Expected struct tag in Go struct")
	}

	// Should contain TypeScript interface
	if !strings.Contains(result, "export interface Example {") {
		t.Error("Expected TypeScript interface")
	}
	if !strings.Contains(result, "name: string") {
		t.Error("Expected field in TypeScript interface")
	}

	// Should have side-by-side table
	if !strings.Contains(result, "<th>Go Source</th>") {
		t.Error("Expected Go Source column")
	}
	if !strings.Contains(result, "<th>TypeScript</th>") {
		t.Error("Expected TypeScript column")
	}
}

func TestGenerateFile_Header(t *testing.T) {
	gen := NewGenerator()
	result := &typegen.Result{
		Types:       map[string]string{"Type1": "content"},
		PackageName: "testpkg",
	}

	output := gen.GenerateFile(result)

	// Should have proper header
	if !strings.Contains(output, "# testpkg Types") {
		t.Error("Expected package name in header")
	}
	if !strings.Contains(output, "DO NOT EDIT") {
		t.Error("Expected DO NOT EDIT warning")
	}
	if !strings.Contains(output, "make types") {
		t.Error("Expected regeneration instructions")
	}
}

func TestFormatFieldType_Basic(t *testing.T) {
	tests := []struct {
		name     string
		expr     ast.Expr
		expected string
	}{
		{
			name:     "simple ident",
			expr:     ast.NewIdent("string"),
			expected: "string",
		},
		{
			name:     "pointer",
			expr:     &ast.StarExpr{X: ast.NewIdent("string")},
			expected: "*string",
		},
		{
			name:     "slice",
			expr:     &ast.ArrayType{Elt: ast.NewIdent("string")},
			expected: "[]string",
		},
		{
			name: "map",
			expr: &ast.MapType{
				Key:   ast.NewIdent("string"),
				Value: ast.NewIdent("int"),
			},
			expected: "map[string]int",
		},
		{
			name: "qualified type",
			expr: &ast.SelectorExpr{
				X:   ast.NewIdent("time"),
				Sel: ast.NewIdent("Time"),
			},
			expected: "time.Time",
		},
		{
			name:     "interface{}",
			expr:     &ast.InterfaceType{},
			expected: "interface{}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatFieldType(tt.expr)
			if result != tt.expected {
				t.Errorf("formatFieldType() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestLanguage(t *testing.T) {
	gen := NewGenerator()
	if gen.Language() != "markdown" {
		t.Errorf("Language() = %q, want %q", gen.Language(), "markdown")
	}
}

func TestFileExtension(t *testing.T) {
	gen := NewGenerator()
	if gen.FileExtension() != "md" {
		t.Errorf("FileExtension() = %q, want %q", gen.FileExtension(), "md")
	}
}
