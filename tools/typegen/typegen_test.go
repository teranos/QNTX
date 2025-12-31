package typegen

import (
	"strings"
	"testing"
)

// TestTypeGeneration validates the core requirement:
// Go types annotated with @ts-export generate correct TypeScript.
func TestTypeGeneration(t *testing.T) {
	tests := []struct {
		name     string
		goSource string
		wantTS   string
	}{
		{
			name: "simple struct with basic types",
			goSource: `package example

// @ts-export
type SimpleMessage struct {
	ID      string  ` + "`json:\"id\"`" + `
	Count   int     ` + "`json:\"count\"`" + `
	Score   float64 ` + "`json:\"score\"`" + `
	Active  bool    ` + "`json:\"active\"`" + `
}`,
			wantTS: `export interface SimpleMessage {
  id: string;
  count: number;
  score: number;
  active: boolean;
}`,
		},
		{
			name: "struct with optional pointer field",
			goSource: `package example

// @ts-export
type OptionalFields struct {
	Required string  ` + "`json:\"required\"`" + `
	Optional *string ` + "`json:\"optional,omitempty\"`" + `
}`,
			wantTS: `export interface OptionalFields {
  required: string;
  optional?: string;
}`,
		},
		{
			name: "struct with nullable pointer field",
			goSource: `package example

// @ts-export
type NullableFields struct {
	Required string  ` + "`json:\"required\"`" + `
	Nullable *string ` + "`json:\"nullable\"`" + `
}`,
			wantTS: `export interface NullableFields {
  required: string;
  nullable: string | null;
}`,
		},
		{
			name: "struct with slice and map",
			goSource: `package example

// @ts-export
type CollectionFields struct {
	Items    []string          ` + "`json:\"items\"`" + `
	Metadata map[string]string ` + "`json:\"metadata\"`" + `
}`,
			wantTS: `export interface CollectionFields {
  items: string[];
  metadata: Record<string, string>;
}`,
		},
		{
			name: "struct with interface{} and map[string]interface{}",
			goSource: `package example

// @ts-export
type DynamicFields struct {
	Data     interface{}            ` + "`json:\"data\"`" + `
	Metadata map[string]interface{} ` + "`json:\"metadata\"`" + `
}`,
			wantTS: `export interface DynamicFields {
  data: unknown;
  metadata: Record<string, unknown>;
}`,
		},
		{
			name: "struct with time.Time",
			goSource: `package example

import "time"

// @ts-export
type TimestampedMessage struct {
	CreatedAt time.Time ` + "`json:\"created_at\"`" + `
}`,
			wantTS: `export interface TimestampedMessage {
  created_at: string;
}`,
		},
		{
			name: "struct with nested type reference",
			goSource: `package example

// @ts-export
type Child struct {
	Name string ` + "`json:\"name\"`" + `
}

// @ts-export
type Parent struct {
	Child Child ` + "`json:\"child\"`" + `
}`,
			wantTS: `export interface Child {
  name: string;
}

export interface Parent {
  child: Child;
}`,
		},
		{
			name: "struct with slice of structs",
			goSource: `package example

// @ts-export
type Item struct {
	ID string ` + "`json:\"id\"`" + `
}

// @ts-export
type Container struct {
	Items []Item ` + "`json:\"items\"`" + `
}`,
			wantTS: `export interface Item {
  id: string;
}

export interface Container {
  items: Item[];
}`,
		},
		{
			name: "only annotated types are exported",
			goSource: `package example

type InternalType struct {
	Secret string ` + "`json:\"secret\"`" + `
}

// @ts-export
type PublicType struct {
	Public string ` + "`json:\"public\"`" + `
}`,
			wantTS: `export interface PublicType {
  public: string;
}`,
		},
		{
			name: "int64 maps to number",
			goSource: `package example

// @ts-export
type NumericTypes struct {
	Small     int     ` + "`json:\"small\"`" + `
	Large     int64   ` + "`json:\"large\"`" + `
	Precision float64 ` + "`json:\"precision\"`" + `
}`,
			wantTS: `export interface NumericTypes {
  small: number;
  large: number;
  precision: number;
}`,
		},
		{
			name: "non-pointer omitempty is optional",
			goSource: `package example

// @ts-export
type MessageWithOptionalNonPointers struct {
	Type    string ` + "`json:\"type\"`" + `
	Content string ` + "`json:\"content\"`" + `
	Error   string ` + "`json:\"error,omitempty\"`" + `
	Count   int    ` + "`json:\"count,omitempty\"`" + `
}`,
			wantTS: `export interface MessageWithOptionalNonPointers {
  type: string;
  content: string;
  error?: string;
  count?: number;
}`,
		},
		{
			name: "string type alias with const values",
			goSource: `package example

// @ts-export
type Status string

const (
	StatusPending   Status = "pending"
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
)`,
			wantTS: `export type Status = "pending" | "active" | "completed";`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Generate(tt.goSource)
			if err != nil {
				t.Fatalf("Generate() error = %v", err)
			}

			// Normalize whitespace for comparison
			gotNorm := normalizeWhitespace(got)
			wantNorm := normalizeWhitespace(tt.wantTS)

			if gotNorm != wantNorm {
				t.Errorf("Generate() mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, tt.wantTS)
			}
		})
	}
}

// TestNoExportedTypes validates behavior when no types are annotated
func TestNoExportedTypes(t *testing.T) {
	goSource := `package example

type InternalOnly struct {
	Field string ` + "`json:\"field\"`" + `
}`

	got, err := Generate(goSource)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	if strings.TrimSpace(got) != "" {
		t.Errorf("Expected empty output for no annotated types, got: %s", got)
	}
}

// TestInvalidGoSource validates error handling for invalid Go code
func TestInvalidGoSource(t *testing.T) {
	goSource := `this is not valid go code`

	_, err := Generate(goSource)
	if err == nil {
		t.Error("Expected error for invalid Go source, got nil")
	}
}

func normalizeWhitespace(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	var result []string
	for _, line := range lines {
		result = append(result, strings.TrimRight(line, " \t"))
	}
	return strings.Join(result, "\n")
}
