package git

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestBuildDependencySummary_NilResult tests handling of nil dependency result
func TestBuildDependencySummary_NilResult(t *testing.T) {
	fields := buildDependencySummary(nil)

	assert.Equal(t, 0, fields["deps_detected"])
	assert.Equal(t, 0, fields["deps_processed"])
	assert.Equal(t, 0, fields["deps_errors"])
	assert.Nil(t, fields["deps_error_details"])
}

// TestBuildDependencySummary_AllSuccess tests successful dependency processing
func TestBuildDependencySummary_AllSuccess(t *testing.T) {
	result := &DepsIngestionResult{
		FilesDetected:  3,
		FilesProcessed: 3,
		ProjectFiles: []ProjectFileResult{
			{Type: "go.mod", Path: "/repo/go.mod", AttestationCount: 5, Error: ""},
			{Type: "package.json", Path: "/repo/package.json", AttestationCount: 10, Error: ""},
			{Type: "Cargo.toml", Path: "/repo/Cargo.toml", AttestationCount: 8, Error: ""},
		},
	}

	fields := buildDependencySummary(result)

	assert.Equal(t, 3, fields["deps_detected"])
	assert.Equal(t, 3, fields["deps_processed"])
	assert.Equal(t, 0, fields["deps_errors"])
	assert.Nil(t, fields["deps_error_details"])
}

// TestBuildDependencySummary_PartialFailure tests partial dependency failures
func TestBuildDependencySummary_PartialFailure(t *testing.T) {
	result := &DepsIngestionResult{
		FilesDetected:  5,
		FilesProcessed: 3,
		ProjectFiles: []ProjectFileResult{
			{Type: "go.mod", Path: "/repo/go.mod", AttestationCount: 5, Error: ""},
			{Type: "package.json", Path: "/repo/package.json", AttestationCount: 0, Error: "invalid JSON syntax"},
			{Type: "Cargo.toml", Path: "/repo/Cargo.toml", AttestationCount: 8, Error: ""},
			{Type: "requirements.txt", Path: "/repo/requirements.txt", AttestationCount: 0, Error: "file not found"},
			{Type: "flake.nix", Path: "/repo/flake.nix", AttestationCount: 3, Error: ""},
		},
	}

	fields := buildDependencySummary(result)

	assert.Equal(t, 5, fields["deps_detected"])
	assert.Equal(t, 3, fields["deps_processed"])
	assert.Equal(t, 2, fields["deps_errors"])

	// Verify error details format
	errorDetails, ok := fields["deps_error_details"].(string)
	assert.True(t, ok, "deps_error_details should be a string")
	assert.Contains(t, errorDetails, "package.json: invalid JSON syntax")
	assert.Contains(t, errorDetails, "requirements.txt: file not found")
}

// TestBuildDependencySummary_AllFailures tests complete dependency failures
func TestBuildDependencySummary_AllFailures(t *testing.T) {
	result := &DepsIngestionResult{
		FilesDetected:  2,
		FilesProcessed: 0,
		ProjectFiles: []ProjectFileResult{
			{Type: "go.mod", Path: "/repo/go.mod", AttestationCount: 0, Error: "permission denied"},
			{Type: "package.json", Path: "/repo/package.json", AttestationCount: 0, Error: "corrupted file"},
		},
	}

	fields := buildDependencySummary(result)

	assert.Equal(t, 2, fields["deps_detected"])
	assert.Equal(t, 0, fields["deps_processed"])
	assert.Equal(t, 2, fields["deps_errors"])

	errorDetails, ok := fields["deps_error_details"].(string)
	assert.True(t, ok)
	assert.Contains(t, errorDetails, "go.mod: permission denied")
	assert.Contains(t, errorDetails, "package.json: corrupted file")
}

// TestBuildDependencySummary_EmptyResult tests empty dependency result
func TestBuildDependencySummary_EmptyResult(t *testing.T) {
	result := &DepsIngestionResult{
		FilesDetected:  0,
		FilesProcessed: 0,
		ProjectFiles:   []ProjectFileResult{},
	}

	fields := buildDependencySummary(result)

	assert.Equal(t, 0, fields["deps_detected"])
	assert.Equal(t, 0, fields["deps_processed"])
	assert.Equal(t, 0, fields["deps_errors"])
	assert.Nil(t, fields["deps_error_details"])
}

// TestBuildDependencySummary_ErrorDetailsFormat tests error message formatting
func TestBuildDependencySummary_ErrorDetailsFormat(t *testing.T) {
	result := &DepsIngestionResult{
		FilesDetected:  3,
		FilesProcessed: 1,
		ProjectFiles: []ProjectFileResult{
			{Type: "go.mod", Path: "/repo/go.mod", Error: "syntax error at line 10"},
			{Type: "Cargo.toml", Path: "/repo/Cargo.toml", Error: ""},
			{Type: "package.json", Path: "/repo/package.json", Error: "missing required field 'name'"},
		},
	}

	fields := buildDependencySummary(result)

	errorDetails := fields["deps_error_details"].(string)

	// Verify format: "type: error; type: error"
	assert.Contains(t, errorDetails, "go.mod: syntax error at line 10")
	assert.Contains(t, errorDetails, "package.json: missing required field 'name'")
	assert.Contains(t, errorDetails, ";")
}
