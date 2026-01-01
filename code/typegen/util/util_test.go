package util

import (
	"go/ast"
	"go/token"
	"testing"
)

// =============================================================================
// Casing tests
// =============================================================================

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"PascalCase", "pascal_case"},
		{"camelCase", "camel_case"},
		{"HTTPSConnection", "https_connection"},
		{"ID", "id"},
		{"UserID", "user_id"},
		{"APIKey", "api_key"},
		{"already_snake", "already_snake"},
		{"", ""},
		{"A", "a"},
		{"AB", "ab"},
		{"ABC", "abc"},
		{"ABCDef", "abc_def"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ToSnakeCase(tt.input)
			if result != tt.expected {
				t.Errorf("ToSnakeCase(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestToPascalCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"snake_case", "SnakeCase"},
		{"kebab-case", "KebabCase"},
		{"mixed_snake-kebab", "MixedSnakeKebab"},
		{"already", "Already"},
		{"", ""},
		{"a", "A"},
		{"a_b", "AB"},
		{"a_b_c", "ABC"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ToPascalCase(tt.input)
			if result != tt.expected {
				t.Errorf("ToPascalCase(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestToCamelCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"snake_case", "snakeCase"},
		{"kebab-case", "kebabCase"},
		{"PascalCase", "pascalCase"},
		{"", ""},
		{"a", "a"},
		{"a_b", "aB"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ToCamelCase(tt.input)
			if result != tt.expected {
				t.Errorf("ToCamelCase(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// Field comment tests
// =============================================================================

func TestExtractFieldComment_DocComment(t *testing.T) {
	field := &ast.Field{
		Doc: &ast.CommentGroup{
			List: []*ast.Comment{
				{Text: "// User's full name"},
			},
		},
	}

	result := ExtractFieldComment(field)
	expected := "User's full name"
	if result != expected {
		t.Errorf("ExtractFieldComment() = %q, want %q", result, expected)
	}
}

func TestExtractFieldComment_InlineComment(t *testing.T) {
	field := &ast.Field{
		Comment: &ast.CommentGroup{
			List: []*ast.Comment{
				{Text: "// Email address"},
			},
		},
	}

	result := ExtractFieldComment(field)
	expected := "Email address"
	if result != expected {
		t.Errorf("ExtractFieldComment() = %q, want %q", result, expected)
	}
}

func TestExtractFieldComment_MultilineDoc(t *testing.T) {
	field := &ast.Field{
		Doc: &ast.CommentGroup{
			List: []*ast.Comment{
				{Text: "// First line"},
				{Text: "// Second line"},
			},
		},
	}

	result := ExtractFieldComment(field)
	expected := "First line Second line"
	if result != expected {
		t.Errorf("ExtractFieldComment() = %q, want %q", result, expected)
	}
}

func TestExtractFieldComment_NoComment(t *testing.T) {
	field := &ast.Field{}

	result := ExtractFieldComment(field)
	if result != "" {
		t.Errorf("ExtractFieldComment() = %q, want empty string", result)
	}
}

func TestExtractFieldComment_DocPrefersOverInline(t *testing.T) {
	field := &ast.Field{
		Doc: &ast.CommentGroup{
			List: []*ast.Comment{
				{Text: "// Doc comment"},
			},
		},
		Comment: &ast.CommentGroup{
			List: []*ast.Comment{
				{Text: "// Inline comment"},
			},
		},
	}

	result := ExtractFieldComment(field)
	expected := "Doc comment"
	if result != expected {
		t.Errorf("ExtractFieldComment() = %q, want %q (Doc should be preferred)", result, expected)
	}
}

func TestCleanCommentText(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"// Comment text", "Comment text"},
		{"/* Block comment */", "Block comment"},
		{"/** JSDoc comment */", "JSDoc comment"},
		{"   // Whitespace   ", "// Whitespace"}, // Leading whitespace preserved, only comment markers stripped
		{"Comment without marker", "Comment without marker"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := CleanCommentText(tt.input)
			if result != tt.expected {
				t.Errorf("CleanCommentText(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// Tag parsing tests
// =============================================================================

func TestParseJSONTag(t *testing.T) {
	tests := []struct {
		name     string
		tag      string
		expected *JSONTagInfo
	}{
		{
			name: "simple",
			tag:  `json:"field_name"`,
			expected: &JSONTagInfo{
				Name:      "field_name",
				Omitempty: false,
				Skip:      false,
			},
		},
		{
			name: "with omitempty",
			tag:  `json:"field_name,omitempty"`,
			expected: &JSONTagInfo{
				Name:      "field_name",
				Omitempty: true,
				Skip:      false,
			},
		},
		{
			name: "skip",
			tag:  `json:"-"`,
			expected: &JSONTagInfo{
				Name:      "-",
				Omitempty: false,
				Skip:      true,
			},
		},
		{
			name:     "no json tag",
			tag:      `db:"field"`,
			expected: nil,
		},
		{
			name:     "nil tag",
			tag:      "",
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tag *ast.BasicLit
			if tt.tag != "" {
				tag = &ast.BasicLit{
					Kind:  token.STRING,
					Value: "`" + tt.tag + "`",
				}
			}

			result := ParseJSONTag(tag)
			if tt.expected == nil {
				if result != nil {
					t.Errorf("ParseJSONTag() = %+v, want nil", result)
				}
				return
			}

			if result == nil {
				t.Fatalf("ParseJSONTag() = nil, want %+v", tt.expected)
			}

			if result.Name != tt.expected.Name {
				t.Errorf("Name = %q, want %q", result.Name, tt.expected.Name)
			}
			if result.Omitempty != tt.expected.Omitempty {
				t.Errorf("Omitempty = %v, want %v", result.Omitempty, tt.expected.Omitempty)
			}
			if result.Skip != tt.expected.Skip {
				t.Errorf("Skip = %v, want %v", result.Skip, tt.expected.Skip)
			}
		})
	}
}

func TestParseCustomTag(t *testing.T) {
	tests := []struct {
		name            string
		tag             string
		tagName         string
		expectedName    string
		expectedOptions map[string]bool
		expectedSkip    bool
	}{
		{
			name:            "simple rusttype",
			tag:             `rusttype:"Vec<String>"`,
			tagName:         "rusttype",
			expectedName:    "Vec<String>",
			expectedOptions: map[string]bool{},
			expectedSkip:    false,
		},
		{
			name:            "with optional",
			tag:             `rusttype:"String,optional"`,
			tagName:         "rusttype",
			expectedName:    "String",
			expectedOptions: map[string]bool{"optional": true},
			expectedSkip:    false,
		},
		{
			name:            "skip",
			tag:             `rusttype:"-"`,
			tagName:         "rusttype",
			expectedName:    "",
			expectedOptions: nil,
			expectedSkip:    true,
		},
		{
			name:            "tstype simple",
			tag:             `tstype:"string"`,
			tagName:         "tstype",
			expectedName:    "string",
			expectedOptions: map[string]bool{},
			expectedSkip:    false,
		},
		{
			name:            "tstype with optional",
			tag:             `tstype:"string,optional"`,
			tagName:         "tstype",
			expectedName:    "string",
			expectedOptions: map[string]bool{"optional": true},
			expectedSkip:    false,
		},
		{
			name:            "no matching tag",
			tag:             `json:"field"`,
			tagName:         "rusttype",
			expectedName:    "",
			expectedOptions: nil,
			expectedSkip:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tag *ast.BasicLit
			if tt.tag != "" {
				tag = &ast.BasicLit{
					Kind:  token.STRING,
					Value: "`" + tt.tag + "`",
				}
			}

			name, options, skip := ParseCustomTag(tag, tt.tagName)

			if name != tt.expectedName {
				t.Errorf("name = %q, want %q", name, tt.expectedName)
			}
			if skip != tt.expectedSkip {
				t.Errorf("skip = %v, want %v", skip, tt.expectedSkip)
			}

			if tt.expectedOptions == nil {
				if options != nil {
					t.Errorf("options = %+v, want nil", options)
				}
			} else {
				if options == nil {
					t.Errorf("options = nil, want %+v", tt.expectedOptions)
				} else {
					for k, v := range tt.expectedOptions {
						if options[k] != v {
							t.Errorf("options[%q] = %v, want %v", k, options[k], v)
						}
					}
				}
			}
		})
	}
}

func TestParseValidateTag(t *testing.T) {
	tests := []struct {
		name        string
		tag         string
		expectedReq bool
		expectedMin int
		expectedMax int
	}{
		{
			name:        "required only",
			tag:         `validate:"required"`,
			expectedReq: true,
			expectedMin: -1,
			expectedMax: -1,
		},
		{
			name:        "required with min",
			tag:         `validate:"required,min=1"`,
			expectedReq: true,
			expectedMin: 1,
			expectedMax: -1,
		},
		{
			name:        "min and max",
			tag:         `validate:"min=1,max=100"`,
			expectedReq: false,
			expectedMin: 1,
			expectedMax: 100,
		},
		{
			name:        "with spaces",
			tag:         `validate:"required, min = 5 , max = 10"`,
			expectedReq: true,
			expectedMin: 5,
			expectedMax: 10,
		},
		{
			name:        "no validate tag",
			tag:         `json:"field"`,
			expectedReq: false,
			expectedMin: -1,
			expectedMax: -1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tag *ast.BasicLit
			if tt.tag != "" {
				tag = &ast.BasicLit{
					Kind:  token.STRING,
					Value: "`" + tt.tag + "`",
				}
			}

			result := ParseValidateTag(tag)

			if tt.tag == `json:"field"` {
				if result != nil {
					t.Errorf("ParseValidateTag() = %+v, want nil", result)
				}
				return
			}

			if result == nil {
				t.Fatalf("ParseValidateTag() = nil, want non-nil")
			}

			if result.Required != tt.expectedReq {
				t.Errorf("Required = %v, want %v", result.Required, tt.expectedReq)
			}
			if result.Min != tt.expectedMin {
				t.Errorf("Min = %d, want %d", result.Min, tt.expectedMin)
			}
			if result.Max != tt.expectedMax {
				t.Errorf("Max = %d, want %d", result.Max, tt.expectedMax)
			}
		})
	}
}

func TestParseValidateTag_InvalidNumbers(t *testing.T) {
	tag := &ast.BasicLit{
		Kind:  token.STRING,
		Value: "`validate:\"min=invalid,max=also_invalid\"`",
	}

	result := ParseValidateTag(tag)

	if result == nil {
		t.Fatal("ParseValidateTag() = nil, want non-nil")
	}

	// Invalid numbers should be ignored (remain at -1)
	if result.Min != -1 {
		t.Errorf("Min = %d, want -1 (invalid should be ignored)", result.Min)
	}
	if result.Max != -1 {
		t.Errorf("Max = %d, want -1 (invalid should be ignored)", result.Max)
	}
}
