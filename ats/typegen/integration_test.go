package typegen_test

import (
	"strings"
	"testing"

	"github.com/teranos/QNTX/ats/typegen"
	"github.com/teranos/QNTX/ats/typegen/typescript"
)

// Helper function to generate TypeScript file from Result
func generateTypeScriptFile(result *typegen.Result) string {
	gen := typescript.NewGenerator()
	return gen.GenerateFile(result)
}

func TestGenerateAs(t *testing.T) {
	// Given: The ats/types package with the As struct
	// When: We generate TypeScript from it
	gen := typescript.NewGenerator()
	result, err := typegen.GenerateFromPackage("github.com/teranos/QNTX/ats/types", gen)
	if err != nil {
		t.Fatalf("typegen.GenerateFromPackage failed: %v", err)
	}

	// Then: The output should contain a valid TypeScript interface for As
	// Check that we got output for the As type
	asTS, ok := result.Types["As"]
	if !ok {
		t.Fatalf("Expected 'As' type in result, got types: %v", keys(result.Types))
	}

	// Verify json tag names are used (not Go field names)
	assertContains(t, asTS, `id: string`)
	assertContains(t, asTS, `subjects: string[]`)
	assertContains(t, asTS, `predicates: string[]`)
	assertContains(t, asTS, `contexts: string[]`)
	assertContains(t, asTS, `actors: string[]`)
	assertContains(t, asTS, `source: string`)

	// Verify time.Time maps to string
	assertContains(t, asTS, `timestamp: string`)
	assertContains(t, asTS, `created_at: string`)

	// Verify map[string]interface{} maps to Record<string, unknown>
	// and omitempty makes it optional
	assertContains(t, asTS, `attributes?: Record<string, unknown>`)

	// Verify it's a proper interface declaration
	if !strings.HasPrefix(strings.TrimSpace(asTS), "export interface As {") {
		t.Errorf("Expected interface declaration, got: %s", asTS[:min(50, len(asTS))])
	}
}

func TestGenerateAsCommand(t *testing.T) {
	// The AsCommand struct should also be generated
	gen := typescript.NewGenerator()
	result, err := typegen.GenerateFromPackage("github.com/teranos/QNTX/ats/types", gen)
	if err != nil {
		t.Fatalf("typegen.GenerateFromPackage failed: %v", err)
	}

	asCommandTS, ok := result.Types["AsCommand"]
	if !ok {
		t.Fatalf("Expected 'AsCommand' type in result, got types: %v", keys(result.Types))
	}

	// AsCommand has similar fields but some are optional (no validate:"required")
	assertContains(t, asCommandTS, `subjects: string[]`)
	assertContains(t, asCommandTS, `predicates: string[]`)
	assertContains(t, asCommandTS, `timestamp: string`)
	assertContains(t, asCommandTS, `attributes?: Record<string, unknown>`)
}

func TestGenerateAxFilter(t *testing.T) {
	// AxFilter has pointer types that should become optional
	gen := typescript.NewGenerator()
	result, err := typegen.GenerateFromPackage("github.com/teranos/QNTX/ats/types", gen)
	if err != nil {
		t.Fatalf("typegen.GenerateFromPackage failed: %v", err)
	}

	axFilterTS, ok := result.Types["AxFilter"]
	if !ok {
		t.Fatalf("Expected 'AxFilter' type in result, got types: %v", keys(result.Types))
	}

	// Pointer fields should be optional with null union
	assertContains(t, axFilterTS, `time_start?: string | null`)
	assertContains(t, axFilterTS, `time_end?: string | null`)

	// Nested struct pointer (OverFilter)
	assertContains(t, axFilterTS, `over_comparison?: OverFilter | null`)
}

func TestGenerateOverFilter(t *testing.T) {
	// OverFilter is a nested type referenced by AxFilter
	gen := typescript.NewGenerator()
	result, err := typegen.GenerateFromPackage("github.com/teranos/QNTX/ats/types", gen)
	if err != nil {
		t.Fatalf("typegen.GenerateFromPackage failed: %v", err)
	}

	overFilterTS, ok := result.Types["OverFilter"]
	if !ok {
		t.Fatalf("Expected 'OverFilter' type in result, got types: %v", keys(result.Types))
	}

	assertContains(t, overFilterTS, `value: number`)
	assertContains(t, overFilterTS, `unit: string`)
	assertContains(t, overFilterTS, `operator: string`)
}

// Helper functions

func assertContains(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("Expected output to contain %q, got:\n%s", needle, haystack)
	}
}

func keys(m map[string]string) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	return result
}

// TestGenerateFile_Output is a visual test to see the full generated output
// Run with: go test -v -run TestGenerateFile_Output
func TestGenerateFile_Output(t *testing.T) {
	gen := typescript.NewGenerator()
	result, err := typegen.GenerateFromPackage("github.com/teranos/QNTX/ats/types", gen)
	if err != nil {
		t.Fatalf("typegen.GenerateFromPackage failed: %v", err)
	}

	output := generateTypeScriptFile(result)
	t.Logf("Generated TypeScript:\n%s", output)
}

// =============================================================================
// Cross-package tests: pulse/async
// =============================================================================

func TestGenerateJob(t *testing.T) {
	// Job is the core async job type from pulse/async
	gen := typescript.NewGenerator()
	result, err := typegen.GenerateFromPackage("github.com/teranos/QNTX/pulse/async", gen)
	if err != nil {
		t.Fatalf("typegen.GenerateFromPackage failed: %v", err)
	}

	jobTS, ok := result.Types["Job"]
	if !ok {
		t.Fatalf("Expected 'Job' type in result, got types: %v", keys(result.Types))
	}

	// Core fields
	assertContains(t, jobTS, `id: string`)
	assertContains(t, jobTS, `handler_name: string`)
	assertContains(t, jobTS, `source: string`)

	// json.RawMessage should map to unknown
	assertContains(t, jobTS, `payload?: unknown`)

	// JobStatus is a type alias - for now it will be the type name
	// TODO: When we add enum support, this should be a union type
	assertContains(t, jobTS, `status: JobStatus`)

	// Nested struct reference
	assertContains(t, jobTS, `progress: Progress`)

	// Pointer to nested struct (optional)
	assertContains(t, jobTS, `pulse_state?: PulseState | null`)

	// time.Time fields
	assertContains(t, jobTS, `created_at: string`)
	assertContains(t, jobTS, `updated_at: string`)

	// Pointer time.Time (optional)
	assertContains(t, jobTS, `started_at?: string | null`)
	assertContains(t, jobTS, `completed_at?: string | null`)
}

func TestGenerateProgress(t *testing.T) {
	gen := typescript.NewGenerator()
	result, err := typegen.GenerateFromPackage("github.com/teranos/QNTX/pulse/async", gen)
	if err != nil {
		t.Fatalf("typegen.GenerateFromPackage failed: %v", err)
	}

	progressTS, ok := result.Types["Progress"]
	if !ok {
		t.Fatalf("Expected 'Progress' type in result, got types: %v", keys(result.Types))
	}

	assertContains(t, progressTS, `current: number`)
	assertContains(t, progressTS, `total: number`)
}

func TestGeneratePulseState(t *testing.T) {
	gen := typescript.NewGenerator()
	result, err := typegen.GenerateFromPackage("github.com/teranos/QNTX/pulse/async", gen)
	if err != nil {
		t.Fatalf("typegen.GenerateFromPackage failed: %v", err)
	}

	pulseStateTS, ok := result.Types["PulseState"]
	if !ok {
		t.Fatalf("Expected 'PulseState' type in result, got types: %v", keys(result.Types))
	}

	assertContains(t, pulseStateTS, `calls_this_minute: number`)
	assertContains(t, pulseStateTS, `calls_remaining: number`)
	assertContains(t, pulseStateTS, `spend_today: number`)
	assertContains(t, pulseStateTS, `is_paused: boolean`)
	assertContains(t, pulseStateTS, `pause_reason?: string`)
}

// =============================================================================
// Cross-package tests: server
// =============================================================================

func TestGenerateServerTypes(t *testing.T) {
	// Server types include WebSocket message types
	gen := typescript.NewGenerator()
	result, err := typegen.GenerateFromPackage("github.com/teranos/QNTX/server", gen)
	if err != nil {
		t.Fatalf("typegen.GenerateFromPackage failed: %v", err)
	}

	// DaemonStatusMessage is a key WebSocket message type
	daemonStatusTS, ok := result.Types["DaemonStatusMessage"]
	if !ok {
		t.Fatalf("Expected 'DaemonStatusMessage' type in result, got types: %v", keys(result.Types))
	}

	assertContains(t, daemonStatusTS, `type: string`)
	assertContains(t, daemonStatusTS, `running: boolean`)
	assertContains(t, daemonStatusTS, `active_jobs: number`)
	assertContains(t, daemonStatusTS, `load_percent: number`)
	assertContains(t, daemonStatusTS, `budget_daily: number`)
}

func TestGenerateJobUpdateMessage(t *testing.T) {
	gen := typescript.NewGenerator()
	result, err := typegen.GenerateFromPackage("github.com/teranos/QNTX/server", gen)
	if err != nil {
		t.Fatalf("typegen.GenerateFromPackage failed: %v", err)
	}

	// JobUpdateMessage references *async.Job from another package
	jobUpdateTS, ok := result.Types["JobUpdateMessage"]
	if !ok {
		t.Fatalf("Expected 'JobUpdateMessage' type in result, got types: %v", keys(result.Types))
	}

	assertContains(t, jobUpdateTS, `type: string`)
	// Cross-package reference: *async.Job
	// For now this will just be "Job" - we may need to handle package prefixes
	assertContains(t, jobUpdateTS, `job?: Job | null`)
	assertContains(t, jobUpdateTS, `metadata: Record<string, unknown>`)
}

// TestGeneratePulseAsync_Output shows all generated types from pulse/async
func TestGeneratePulseAsync_Output(t *testing.T) {
	gen := typescript.NewGenerator()
	result, err := typegen.GenerateFromPackage("github.com/teranos/QNTX/pulse/async", gen)
	if err != nil {
		t.Fatalf("typegen.GenerateFromPackage failed: %v", err)
	}

	output := generateTypeScriptFile(result)
	t.Logf("Generated TypeScript for pulse/async:\n%s", output)
}

// =============================================================================
// Enum support tests
// =============================================================================

func TestGenerateJobStatusEnum(t *testing.T) {
	// JobStatus is a type alias with const values - should become a union type
	gen := typescript.NewGenerator()
	result, err := typegen.GenerateFromPackage("github.com/teranos/QNTX/pulse/async", gen)
	if err != nil {
		t.Fatalf("typegen.GenerateFromPackage failed: %v", err)
	}

	// Should have a type alias for JobStatus
	jobStatusTS, ok := result.Types["JobStatus"]
	if !ok {
		t.Fatalf("Expected 'JobStatus' type in result, got types: %v", keys(result.Types))
	}

	// Should be a union of string literals
	assertContains(t, jobStatusTS, `'queued'`)
	assertContains(t, jobStatusTS, `'running'`)
	assertContains(t, jobStatusTS, `'paused'`)
	assertContains(t, jobStatusTS, `'completed'`)
	assertContains(t, jobStatusTS, `'failed'`)
	assertContains(t, jobStatusTS, `'cancelled'`)

	// Should be a type alias, not interface
	if !strings.HasPrefix(strings.TrimSpace(jobStatusTS), "export type JobStatus =") {
		t.Errorf("Expected type alias declaration, got: %s", jobStatusTS)
	}
}

// =============================================================================
// Symbol package (sym) tests - consts, arrays, maps
// =============================================================================

func TestGenerateSymConsts(t *testing.T) {
	// sym package has untyped const declarations (const I = "⍟")
	gen := typescript.NewGenerator()
	result, err := typegen.GenerateFromPackage("github.com/teranos/QNTX/sym", gen)
	if err != nil {
		t.Fatalf("typegen.GenerateFromPackage failed: %v", err)
	}

	// Should extract untyped const values
	if len(result.Consts) == 0 {
		t.Fatalf("Expected const values, got none")
	}

	// Check for some known symbols
	if val, ok := result.Consts["Pulse"]; !ok || val == "" {
		t.Errorf("Expected 'Pulse' const to be extracted, got: %v", val)
	}
}

func TestGenerateSymArrays(t *testing.T) {
	// sym package has slice literal (var PaletteOrder = []string{...})
	gen := typescript.NewGenerator()
	result, err := typegen.GenerateFromPackage("github.com/teranos/QNTX/sym", gen)
	if err != nil {
		t.Fatalf("typegen.GenerateFromPackage failed: %v", err)
	}

	// Should extract array literals
	if len(result.Arrays) == 0 {
		t.Fatalf("Expected array values, got none")
	}

	// Check for PaletteOrder
	if palette, ok := result.Arrays["PaletteOrder"]; !ok || len(palette) == 0 {
		t.Errorf("Expected 'PaletteOrder' array to be extracted")
	}
}

func TestGenerateSymMaps(t *testing.T) {
	// sym package has map literals (var SymbolToCommand = map[string]string{...})
	gen := typescript.NewGenerator()
	result, err := typegen.GenerateFromPackage("github.com/teranos/QNTX/sym", gen)
	if err != nil {
		t.Fatalf("typegen.GenerateFromPackage failed: %v", err)
	}

	// Should extract map literals
	if len(result.Maps) == 0 {
		t.Fatalf("Expected map values, got none")
	}

	// Check for known maps
	if _, ok := result.Maps["SymbolToCommand"]; !ok {
		t.Errorf("Expected 'SymbolToCommand' map to be extracted")
	}
	if _, ok := result.Maps["CommandToSymbol"]; !ok {
		t.Errorf("Expected 'CommandToSymbol' map to be extracted")
	}
	if _, ok := result.Maps["CommandDescriptions"]; !ok {
		t.Errorf("Expected 'CommandDescriptions' map to be extracted")
	}
}

func TestGenerateSymPackage(t *testing.T) {
	// Full integration test for sym package
	gen := typescript.NewGenerator()
	result, err := typegen.GenerateFromPackage("github.com/teranos/QNTX/sym", gen)
	if err != nil {
		t.Fatalf("typegen.GenerateFromPackage failed: %v", err)
	}

	// Generate full TypeScript file
	output := generateTypeScriptFile(result)
	t.Logf("Generated TypeScript for sym:\n%s", output)

	// Verify const exports
	assertContains(t, output, `export const`)

	// Verify array exports
	assertContains(t, output, `string[]`)

	// Verify map exports
	assertContains(t, output, `Record<string, string>`)

	// Verify file header
	assertContains(t, output, `// Source package: sym`)
}

// =============================================================================
// Note: TypeScript-specific unit tests are in typescript/generator_test.go
// These are integration tests that verify the full pipeline
// =============================================================================
