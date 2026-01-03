// Package github provides types and utilities for GitHub PR integration,
// including fetching fix suggestions from PR comments and PR auto-detection.
package github

import (
	"time"

	"github.com/teranos/QNTX/code/ast"
)

// FixSuggestion represents a single code fix suggestion from AI code review.
// It contains the location, description, and severity of an identified issue.
type FixSuggestion struct {
	ID             string `json:"id"`
	Category       string `json:"category"` // e.g., "SEC" (security), "DRY" (don't repeat yourself), "GO" (Go idioms), "PERF" (performance)
	Title          string `json:"title"`
	File           string `json:"file"`
	StartLine      int    `json:"start_line"`
	EndLine        int    `json:"end_line"`
	StartCharacter int    `json:"start_character,omitempty"` // 0-based column position (optional, PR #173)
	Issue          string `json:"issue"`
	Severity       string `json:"severity"`
	Patch          string `json:"patch"`
}

// FixContext provides context for AI fix generation.
// It includes the problematic code, surrounding lines for context,
// and the full file content for reference.
type FixContext struct {
	ProblematicCode string // The actual problematic lines
	SurroundingCode string // Context lines before/after
	FileContent     string // Full file for reference
}

// FixResult represents the result of AI code transformation generation.
// NOTE: Uses semantic AST transformations, not text patches.
// The "Fix" prefix will become "Code" in future versions (internal/code package).
type FixResult struct {
	// AST transformation (semantic code change)
	// This is format-preserving and whitespace-agnostic
	Transform *ast.ASTTransformation `json:"transform,omitempty"`

	// Metadata
	Reasoning  string  `json:"reasoning"`  // Explanation of what was changed and why
	Confidence float64 `json:"confidence"` // AI's confidence in the fix (0.0-1.0)
}

// PatchResult stores the result of patch generation for a fix suggestion.
// It tracks both successful patch generation and any errors encountered.
type PatchResult struct {
	Suggestion *FixSuggestion // The original fix suggestion
	Patch      *FixResult     // The generated patch (nil if error)
	Error      error          // Any error during patch generation
	PatchID    int64          // Database ID if saved (0 if not saved)
}

// CachedPatch represents a patch stored in the database.
// It provides quick access to previously generated patches
// without regenerating them via AI.
type CachedPatch struct {
	ID         int64     // Database primary key
	FixID      string    // Fix suggestion ID (e.g., "FIX-1")
	FilePath   string    // Path to the file being patched
	Issue      string    // Description of the issue
	Severity   string    // Severity level (low, medium, high)
	Confidence float64   // AI confidence in the fix (0.0-1.0)
	CreatedAt  time.Time // When the patch was generated
}

// StalenessInfo contains information about whether code has been modified after review.
// It helps determine if a patch is still applicable or needs regeneration.
type StalenessInfo struct {
	IsStale          bool   // Whether the code has changed since review
	ReviewTime       string // When the review was performed
	FileModTime      string // Last modification time of the file
	FileCommits      int    // Commits to the specific file
	PackageCommits   int    // Commits to files in the same package/directory
	ElsewhereCommits int    // Commits to files outside the package
}
