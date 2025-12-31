package typegen

import (
	"strings"
	"testing"
)

func TestGenerateAs(t *testing.T) {
	// Given: The ats/types package with the As struct
	// When: We generate TypeScript from it
	result, err := GenerateFromPackage("github.com/teranos/QNTX/ats/types")
	if err != nil {
		t.Fatalf("GenerateFromPackage failed: %v", err)
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
	result, err := GenerateFromPackage("github.com/teranos/QNTX/ats/types")
	if err != nil {
		t.Fatalf("GenerateFromPackage failed: %v", err)
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
	result, err := GenerateFromPackage("github.com/teranos/QNTX/ats/types")
	if err != nil {
		t.Fatalf("GenerateFromPackage failed: %v", err)
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
	result, err := GenerateFromPackage("github.com/teranos/QNTX/ats/types")
	if err != nil {
		t.Fatalf("GenerateFromPackage failed: %v", err)
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
	result, err := GenerateFromPackage("github.com/teranos/QNTX/ats/types")
	if err != nil {
		t.Fatalf("GenerateFromPackage failed: %v", err)
	}

	output := GenerateFile(result)
	t.Logf("Generated TypeScript:\n%s", output)
}
