package typescript

import (
	"reflect"
	"testing"
)

func TestSplitByDelimiters(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		delimiters string
		want       []string
	}{
		{
			name:       "basic split by space",
			input:      "hello world foo",
			delimiters: " ",
			want:       []string{"hello", "world", "foo"},
		},
		{
			name:       "split by multiple delimiters",
			input:      "foo:bar;baz|qux",
			delimiters: ":;|",
			want:       []string{"foo", "bar", "baz", "qux"},
		},
		{
			name:       "TypeScript field type pattern",
			input:      "field: Type[]",
			delimiters: " :[]",
			want:       []string{"field", "Type"},
		},
		{
			name:       "optional field with union",
			input:      "field?: string | null",
			delimiters: " :?|",
			want:       []string{"field", "string", "null"},
		},
		{
			name:       "generic type",
			input:      "Record<string, Type>",
			delimiters: "<>, ",
			want:       []string{"Record", "string", "Type"},
		},
		{
			name:       "consecutive delimiters",
			input:      "foo  bar",
			delimiters: " ",
			want:       []string{"foo", "bar"},
		},
		{
			name:       "empty string",
			input:      "",
			delimiters: " ",
			want:       nil,
		},
		{
			name:       "only delimiters",
			input:      "   ",
			delimiters: " ",
			want:       nil,
		},
		{
			name:       "no delimiters found",
			input:      "foobar",
			delimiters: " ",
			want:       []string{"foobar"},
		},
		{
			name:       "mixed whitespace and symbols",
			input:      "type: { foo: string; bar: number }",
			delimiters: " :;{}",
			want:       []string{"type", "foo", "string", "bar", "number"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitByDelimiters(tt.input, tt.delimiters)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitByDelimiters() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsAlphanumeric(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "lowercase letters",
			input: "hello",
			want:  true,
		},
		{
			name:  "uppercase letters",
			input: "HELLO",
			want:  true,
		},
		{
			name:  "PascalCase type name",
			input: "JobStatus",
			want:  true,
		},
		{
			name:  "numbers only",
			input: "12345",
			want:  true,
		},
		{
			name:  "letters and numbers",
			input: "Type123",
			want:  true,
		},
		{
			name:  "with underscore",
			input: "job_status",
			want:  false,
		},
		{
			name:  "with hyphen",
			input: "job-status",
			want:  false,
		},
		{
			name:  "with dot",
			input: "time.Time",
			want:  false,
		},
		{
			name:  "with space",
			input: "job status",
			want:  false,
		},
		{
			name:  "with special chars",
			input: "Type[]",
			want:  false,
		},
		{
			name:  "empty string",
			input: "",
			want:  true,
		},
		{
			name:  "unicode letters",
			input: "café",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isAlphanumeric(tt.input)
			if got != tt.want {
				t.Errorf("isAlphanumeric(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
