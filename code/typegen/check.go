package typegen

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CheckResult holds the result of a types check
type CheckResult struct {
	UpToDate    bool
	Differences map[string][]string // language -> files with differences
}

// CompareDirectories compares generated types in tempDir with existing types.
// Returns a CheckResult indicating which files differ.
//
// Directory structure expected:
//   tempDir/typescript/   -> compares with types/generated/typescript/
//   tempDir/rust/         -> compares with types/generated/rust/ (ignores metadata)
//   tempDir/markdown/     -> compares with docs/types/
func CompareDirectories(tempDir string) (*CheckResult, error) {
	differences := make(map[string][]string)

	// Check TypeScript
	if diffs := compareDirectory(
		filepath.Join(tempDir, "typescript"),
		"types/generated/typescript",
		false, // Don't ignore metadata for TypeScript
	); len(diffs) > 0 {
		differences["TypeScript"] = diffs
	}

	// Check Rust (ignore metadata comments)
	if diffs := compareDirectory(
		filepath.Join(tempDir, "rust"),
		"types/generated/rust",
		true, // Ignore metadata for Rust
	); len(diffs) > 0 {
		differences["Rust"] = diffs
	}

	// Check Markdown
	if diffs := compareDirectory(
		filepath.Join(tempDir, "markdown"),
		"docs/types",
		false, // Don't ignore metadata for Markdown
	); len(diffs) > 0 {
		differences["Markdown"] = diffs
	}

	return &CheckResult{
		UpToDate:    len(differences) == 0,
		Differences: differences,
	}, nil
}

// compareDirectory compares two directories and returns files with differences.
// If ignoreMetadata is true, ignores lines starting with "// Source last modified:" or "// Source version:".
func compareDirectory(tempDir, existingDir string, ignoreMetadata bool) []string {
	var diffs []string

	// Check if temp directory exists
	if _, err := os.Stat(tempDir); os.IsNotExist(err) {
		return diffs
	}

	// Walk through temp directory
	filepath.Walk(tempDir, func(tempPath string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		// Skip certain files
		baseName := filepath.Base(tempPath)
		if shouldSkipFile(baseName) {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(tempDir, tempPath)
		if err != nil {
			return err
		}

		// Corresponding existing file
		existingPath := filepath.Join(existingDir, relPath)

		// Check if files differ
		different, err := filesAreDifferent(tempPath, existingPath, ignoreMetadata)
		if err != nil {
			diffs = append(diffs, relPath+" (error: "+err.Error()+")")
		} else if different {
			diffs = append(diffs, relPath)
		}

		return nil
	})

	return diffs
}

// shouldSkipFile returns true if the file should be skipped during comparison.
func shouldSkipFile(basename string) bool {
	skip := []string{
		"README.md",
		"Cargo.lock",
		"Cargo.toml",
		"mod.rs",
		"lib.rs",
		"target", // Rust build directory
	}

	for _, s := range skip {
		if basename == s {
			return true
		}
	}

	return false
}

// filesAreDifferent compares two files, optionally ignoring metadata lines.
func filesAreDifferent(file1, file2 string, ignoreMetadata bool) (bool, error) {
	// Read both files
	content1, err := os.ReadFile(file1)
	if err != nil {
		return false, fmt.Errorf("failed to read %s: %w", file1, err)
	}

	content2, err := os.ReadFile(file2)
	if err != nil {
		return false, fmt.Errorf("failed to read %s: %w", file2, err)
	}

	// If not ignoring metadata, do simple comparison
	if !ignoreMetadata {
		return !bytes.Equal(content1, content2), nil
	}

	// Compare line by line, ignoring metadata
	lines1 := filterMetadataLines(content1)
	lines2 := filterMetadataLines(content2)

	return lines1 != lines2, nil
}

// filterMetadataLines removes metadata comment lines from content.
// These are the "// Source last modified:" and "// Source version:" lines
// that change on every generation and don't represent actual type changes.
// Returns empty string if scanner encounters an error.
func filterMetadataLines(content []byte) string {
	var result strings.Builder
	scanner := bufio.NewScanner(bytes.NewReader(content))

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Skip metadata comment lines
		if strings.HasPrefix(trimmed, "// Source last modified:") ||
			strings.HasPrefix(trimmed, "// Source version:") {
			continue
		}

		result.WriteString(line)
		result.WriteString("\n")
	}

	// Check for scanner errors (e.g., lines too long)
	if err := scanner.Err(); err != nil {
		// Return empty string on error - will cause comparison to fail
		// This is safer than silently ignoring the error
		return ""
	}

	return result.String()
}
