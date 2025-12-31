package git

import (
	"os"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// TestGitIxProcessor_ProcessCurrentRepository tests processing the actual qntx repository
func TestGitIxProcessor_ProcessCurrentRepository(t *testing.T) {
	// Skip if not in a git repository (e.g., CI without .git)
	if _, err := os.Stat("../../.git"); os.IsNotExist(err) {
		t.Skip("Not in a git repository, skipping test")
	}

	db := qntxtest.CreateTestDB(t)

	processor := NewGitIxProcessor(db, true, "", 0, nil) // dry-run mode

	// Process the qntx repository (from ixgest/git/ we need to go up two levels)
	result, err := processor.ProcessGitRepository("../..")
	require.NoError(t, err, "Failed to process repository")

	// Validate results
	assert.True(t, result.Success, "Processing should succeed")
	assert.True(t, result.CommitsProcessed > 0, "Should process at least one commit")
	assert.True(t, result.TotalAttestations > 0, "Should generate attestations")
	// Note: BranchesProcessed may be 0 in CI environments with shallow clones

	// Verify structure
	assert.Equal(t, "../..", result.RepositoryPath)
	assert.True(t, result.DryRun, "Should be in dry-run mode")

	t.Logf("Processed %d commits and %d branches, generated %d attestations",
		result.CommitsProcessed, result.BranchesProcessed, result.TotalAttestations)
}

// TestGitIxProcessor_IsGitRepository tests repository detection
func TestGitIxProcessor_IsGitRepository(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{
			name:     "valid git repository",
			path:     "../..",
			expected: true,
		},
		{
			name:     "non-git directory",
			path:     "/tmp",
			expected: false,
		},
		{
			name:     "non-existent path",
			path:     "/nonexistent/path/to/repo",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsGitRepository(tt.path)
			assert.Equal(t, tt.expected, result, "IsGitRepository(%s) = %v, want %v", tt.path, result, tt.expected)
		})
	}
}

// TestGitIxProcessor_TruncateMessage tests commit message truncation
func TestGitIxProcessor_TruncateMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "short message",
			input:    "Fix bug",
			expected: "Fix bug",
		},
		{
			name:     "exactly 80 chars",
			input:    "1234567890123456789012345678901234567890123456789012345678901234567890123456789",
			expected: "1234567890123456789012345678901234567890123456789012345678901234567890123456789",
		},
		{
			name:     "over 80 chars",
			input:    "12345678901234567890123456789012345678901234567890123456789012345678901234567890EXTRA",
			expected: "12345678901234567890123456789012345678901234567890123456789012345678901234567...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateMessage(tt.input)
			assert.Equal(t, tt.expected, result)
			assert.LessOrEqual(t, len(result), 80, "Truncated message should be <= 80 chars")
		})
	}
}

// TestGitIxProcessor_CommitAttestations tests attestation generation for a specific commit
func TestGitIxProcessor_CommitAttestations(t *testing.T) {
	// Skip if not in a git repository
	if _, err := os.Stat("../../.git"); os.IsNotExist(err) {
		t.Skip("Not in a git repository, skipping test")
	}

	db := qntxtest.CreateTestDB(t)

	repo, err := git.PlainOpen("../..")
	require.NoError(t, err, "Failed to open repository")

	// Get HEAD commit
	ref, err := repo.Head()
	require.NoError(t, err, "Failed to get HEAD")

	commit, err := repo.CommitObject(ref.Hash())
	require.NoError(t, err, "Failed to get commit object")

	// Create processor
	processor := NewGitIxProcessor(db, false, "", 0, nil)

	// Process single commit
	result, err := processor.processCommit(commit)
	require.NoError(t, err, "Failed to process commit")

	// Verify commit result structure
	assert.Equal(t, commit.Hash.String(), result.Hash, "Full hash should match")
	assert.Equal(t, commit.Hash.String()[:7], result.ShortHash, "Short hash should be 7 chars")
	assert.Equal(t, commit.Author.Name, result.Author, "Author should match")
	assert.Greater(t, result.AttestationCount, 0, "Should generate attestations")

	// Verify attestation types generated
	// Every commit should have at least:
	// - is_commit
	// - node_type
	// - authored_by
	// - author node_type
	// - has_message
	// - committed_at
	minExpectedAttestations := 6
	assert.GreaterOrEqual(t, result.AttestationCount, minExpectedAttestations,
		"Should generate at least %d attestations (basic + node types)", minExpectedAttestations)

	t.Logf("Generated %d attestations for commit %s", result.AttestationCount, result.ShortHash)
}

// TestGitIxProcessor_BranchAttestations tests attestation generation for branches
func TestGitIxProcessor_BranchAttestations(t *testing.T) {
	// Skip if not in a git repository
	if _, err := os.Stat("../../.git"); os.IsNotExist(err) {
		t.Skip("Not in a git repository, skipping test")
	}

	db := qntxtest.CreateTestDB(t)

	repo, err := git.PlainOpen("../..")
	require.NoError(t, err, "Failed to open repository")

	// Create processor
	processor := NewGitIxProcessor(db, false, "", 0, nil)

	// Process branches
	results, err := processor.processBranches(repo)
	require.NoError(t, err, "Failed to process branches")

	// CI environments may have no branches (shallow clone, detached HEAD)
	if len(results) == 0 {
		t.Skip("No branches found (likely CI shallow clone)")
	}

	// Verify first branch structure
	branch := results[0]
	assert.NotEmpty(t, branch.Name, "Branch should have a name")
	assert.NotEmpty(t, branch.CommitHash, "Branch should point to a commit")
	assert.Equal(t, 40, len(branch.CommitHash), "Commit hash should be 40 chars (full SHA-1)")
	assert.Greater(t, branch.AttestationCount, 0, "Should generate attestations")

	// Every branch should generate at least 2 attestations:
	// - points_to
	// - node_type
	assert.GreaterOrEqual(t, branch.AttestationCount, 2, "Should generate at least 2 attestations per branch")

	t.Logf("Processed %d branches", len(results))
}

// TestGitIxProcessor_ErrorHandling tests error cases
func TestGitIxProcessor_ErrorHandling(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	processor := NewGitIxProcessor(db, true, "", 0, nil)

	tests := []struct {
		name        string
		path        string
		expectError bool
	}{
		{
			name:        "non-existent repository",
			path:        "/nonexistent/repo",
			expectError: true,
		},
		{
			name:        "not a git repository",
			path:        "/tmp",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := processor.ProcessGitRepository(tt.path)
			if tt.expectError {
				assert.Error(t, err, "Should return error for invalid repository")
			} else {
				assert.NoError(t, err, "Should not return error for valid repository")
			}
		})
	}
}

// TestGitIxProcessor_DryRunMode tests that dry-run doesn't write to database
func TestGitIxProcessor_DryRunMode(t *testing.T) {
	// Skip if not in a git repository
	if _, err := os.Stat("../../.git"); os.IsNotExist(err) {
		t.Skip("Not in a git repository, skipping test")
	}

	db := qntxtest.CreateTestDB(t)

	// Process in dry-run mode
	processor := NewGitIxProcessor(db, true, "", 0, nil)
	result, err := processor.ProcessGitRepository("../..")
	require.NoError(t, err, "Dry-run processing should succeed")

	// Verify no attestations were written to database
	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM attestations").Scan(&count)
	require.NoError(t, err)

	assert.Equal(t, 0, count, "Dry-run should not write to database")
	assert.True(t, result.DryRun, "Result should indicate dry-run mode")
	assert.Greater(t, result.TotalAttestations, 0, "Should still report attestations that would be generated")
}

// TestGitIxProcessor_NewGitIxProcessor tests processor initialization
func TestGitIxProcessor_NewGitIxProcessor(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	tests := []struct {
		name          string
		actor         string
		expectedActor string
	}{
		{
			name:          "with custom actor",
			actor:         "custom@actor",
			expectedActor: "custom@actor",
		},
		{
			name:          "with empty actor (default)",
			actor:         "",
			expectedActor: "ixgest-git@repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			processor := NewGitIxProcessor(db, false, tt.actor, 0, nil)
			assert.Equal(t, tt.expectedActor, processor.defaultActor)
			assert.Equal(t, db, processor.db)
			assert.False(t, processor.dryRun)
			assert.Equal(t, 0, processor.verbosity)
		})
	}
}
