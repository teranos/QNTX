package typescript

import (
	"reflect"
	"strings"
	"testing"
)

func TestStripComments(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "JSDoc comment",
			input: "/** This is a comment */\ncode here",
			want:  "\ncode here",
		},
		{
			name:  "line comment",
			input: "code // comment\nmore code",
			want:  "code \nmore code",
		},
		{
			name:  "multiple JSDoc comments",
			input: "/** comment 1 */\ncode\n/** comment 2 */\nmore",
			want:  "\ncode\n\nmore",
		},
		{
			name:  "JSDoc in middle of line",
			input: "before /** comment */ after",
			want:  "before  after",
		},
		{
			name:  "line comment at end",
			input: "code // comment",
			want:  "code ",
		},
		{
			name:  "multiple line comments",
			input: "// line 1\n// line 2\ncode",
			want:  "\n\ncode",
		},
		{
			name:  "mixed comments",
			input: "/** JSDoc */\ncode // inline\nmore",
			want:  "\ncode \nmore",
		},
		{
			name:  "no comments",
			input: "just code here",
			want:  "just code here",
		},
		{
			name:  "field with JSDoc",
			input: "  /** Status message */\n  status: string;",
			want:  "  \n  status: string;",
		},
		{
			name:  "incomplete JSDoc at end",
			input: "code /** comment without close",
			want:  "code /** comment without close", // Incomplete comment is not stripped
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripComments(tt.input)
			if got != tt.want {
				t.Errorf("stripComments() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractTypeNames(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "simple interface field",
			input: "field: Type;",
			want:  []string{"Type"},
		},
		{
			name:  "array type",
			input: "items: Item[];",
			want:  []string{"Item"},
		},
		{
			name:  "optional with union",
			input: "job?: Job | null;",
			want:  []string{"Job"},
		},
		{
			name:  "multiple fields",
			input: "job: Job;\nstatus: Status;\nexecution: Execution;",
			want:  []string{"Job", "Status", "Execution"},
		},
		{
			name:  "with JSDoc comment",
			input: "/** Status message */\nstatus: string;",
			want:  nil, // "Status" in comment should be stripped
		},
		{
			name:  "generic Record type",
			input: "metadata: Record<string, unknown>;",
			want:  []string{"Record"}, // extractTypeNames finds it; isBuiltinType filters it later
		},
		{
			name:  "custom generic",
			input: "data: Map<string, Job>;",
			want:  []string{"Map", "Job"},
		},
		{
			name:  "no PascalCase types",
			input: "field: string;\ncount: number;",
			want:  nil,
		},
		{
			name:  "mixed with built-ins",
			input: "job: Job;\ncount: number;\nstatus: Status;",
			want:  []string{"Job", "Status"},
		},
		{
			name:  "duplicate types",
			input: "job1: Job;\njob2: Job;",
			want:  []string{"Job", "Job"}, // extractTypeNames doesn't dedupe
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractTypeNames(tt.input)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractTypeNames() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsBuiltinType(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		want     bool
	}{
		{
			name:     "Record is built-in",
			typeName: "Record",
			want:     true,
		},
		{
			name:     "custom type",
			typeName: "Job",
			want:     false,
		},
		{
			name:     "another custom type",
			typeName: "Status",
			want:     false,
		},
		{
			name:     "empty string",
			typeName: "",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isBuiltinType(tt.typeName)
			if got != tt.want {
				t.Errorf("isBuiltinType(%q) = %v, want %v", tt.typeName, got, tt.want)
			}
		})
	}
}

func TestFindRequiredImports(t *testing.T) {
	tests := []struct {
		name           string
		output         string
		currentPackage string
		typeToPackage  map[string]string
		want           map[string][]string
	}{
		{
			name: "single cross-package reference",
			output: `export interface Message {
  job: Job;
}`,
			currentPackage: "server",
			typeToPackage: map[string]string{
				"Job": "async",
			},
			want: map[string][]string{
				"async": {"Job"},
			},
		},
		{
			name: "multiple types from same package",
			output: `export interface Message {
  job: Job;
  status: JobStatus;
}`,
			currentPackage: "server",
			typeToPackage: map[string]string{
				"Job":       "async",
				"JobStatus": "async",
			},
			want: map[string][]string{
				"async": {"Job", "JobStatus"},
			},
		},
		{
			name: "types from different packages",
			output: `export interface Message {
  job: Job;
  budget: Budget;
  execution: Execution;
}`,
			currentPackage: "server",
			typeToPackage: map[string]string{
				"Job":       "async",
				"Budget":    "budget",
				"Execution": "schedule",
			},
			want: map[string][]string{
				"async":    {"Job"},
				"budget":   {"Budget"},
				"schedule": {"Execution"},
			},
		},
		{
			name: "same package - no imports",
			output: `export interface Message {
  job: Job;
}`,
			currentPackage: "async",
			typeToPackage: map[string]string{
				"Job": "async",
			},
			want: map[string][]string{},
		},
		{
			name: "built-in types - no imports",
			output: `export interface Message {
  metadata: Record<string, unknown>;
}`,
			currentPackage: "server",
			typeToPackage:  map[string]string{},
			want:           map[string][]string{},
		},
		{
			name: "unknown types - no imports",
			output: `export interface Message {
  data: SomeUnknownType;
}`,
			currentPackage: "server",
			typeToPackage:  map[string]string{},
			want:           map[string][]string{},
		},
		{
			name: "array and union types",
			output: `export interface Message {
  jobs: Job[];
  status?: Status | null;
}`,
			currentPackage: "server",
			typeToPackage: map[string]string{
				"Job":    "async",
				"Status": "budget",
			},
			want: map[string][]string{
				"async":  {"Job"},
				"budget": {"Status"},
			},
		},
		{
			name: "with JSDoc comments",
			output: `export interface Message {
  /** Job details */
  job: Job;
}`,
			currentPackage: "server",
			typeToPackage: map[string]string{
				"Job": "async",
			},
			want: map[string][]string{
				"async": {"Job"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindRequiredImports(tt.output, tt.currentPackage, tt.typeToPackage)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FindRequiredImports() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAddImportsToOutput(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		imports map[string][]string
		want    string
	}{
		{
			name: "add single import",
			output: `/* eslint-disable */
// Code generated

export interface Message {
  job: Job;
}`,
			imports: map[string][]string{
				"async": {"Job"},
			},
			want: `/* eslint-disable */
// Code generated

import { Job } from './async';

export interface Message {
  job: Job;
}`,
		},
		{
			name: "add multiple imports from one package",
			output: `/* eslint-disable */
// Code generated

export interface Message {
  job: Job;
  status: JobStatus;
}`,
			imports: map[string][]string{
				"async": {"Job", "JobStatus"},
			},
			want: `/* eslint-disable */
// Code generated

import { Job, JobStatus } from './async';

export interface Message {
  job: Job;
  status: JobStatus;
}`,
		},
		{
			name: "add imports from multiple packages",
			output: `/* eslint-disable */
// Code generated

export interface Message {
  job: Job;
  budget: Budget;
}`,
			imports: map[string][]string{
				"async":  {"Job"},
				"budget": {"Budget"},
			},
			want: `/* eslint-disable */
// Code generated

import { Job } from './async';
import { Budget } from './budget';

export interface Message {
  job: Job;
  budget: Budget;
}`,
		},
		{
			name: "no imports - return unchanged",
			output: `/* eslint-disable */
// Code generated

export interface Message {
  name: string;
}`,
			imports: map[string][]string{},
			want: `/* eslint-disable */
// Code generated

export interface Message {
  name: string;
}`,
		},
		{
			name: "deterministic import order (sorted)",
			output: `/* eslint-disable */
// Code generated

export interface Message {
}`,
			imports: map[string][]string{
				"schedule": {"Execution"},
				"async":    {"Job"},
				"budget":   {"Budget"},
			},
			want: `/* eslint-disable */
// Code generated

import { Job } from './async';
import { Budget } from './budget';
import { Execution } from './schedule';

export interface Message {
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := AddImportsToOutput(tt.output, tt.imports)
			// Normalize newlines for comparison
			gotNorm := strings.ReplaceAll(got, "\r\n", "\n")
			wantNorm := strings.ReplaceAll(tt.want, "\r\n", "\n")
			if gotNorm != wantNorm {
				t.Errorf("AddImportsToOutput() =\n%s\n\nwant:\n%s", gotNorm, wantNorm)
			}
		})
	}
}
