package typegen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompareDirectories_RustMetadataFiltering(t *testing.T) {
	tempDir := t.TempDir()
	existingDir := t.TempDir()

	// Rust files with different metadata but identical code
	rustContent1 := `// Source last modified: 2025-12-25
// Source version: abc123
pub struct User { pub id: String }
`
	rustContent2 := `// Source last modified: 2025-12-27
// Source version: xyz789
pub struct User { pub id: String }
`

	os.MkdirAll(filepath.Join(tempDir, "rust"), 0755)
	os.MkdirAll(filepath.Join(existingDir, "rust"), 0755)
	os.WriteFile(filepath.Join(tempDir, "rust", "user.rs"), []byte(rustContent1), 0644)
	os.WriteFile(filepath.Join(existingDir, "rust", "user.rs"), []byte(rustContent2), 0644)

	// Should be identical with metadata filtering
	diffs := compareDirectory(
		filepath.Join(tempDir, "rust"),
		filepath.Join(existingDir, "rust"),
		true,
	)

	if len(diffs) != 0 {
		t.Errorf("Expected no differences with metadata filtering, got: %v", diffs)
	}
}

func TestCompareDirectories_RustFunctionalChange(t *testing.T) {
	tempDir := t.TempDir()
	existingDir := t.TempDir()

	// Same metadata but different code
	rustContent1 := `// Source last modified: 2025-12-25
pub struct User { pub id: String }
`
	rustContent2 := `// Source last modified: 2025-12-25
pub struct User { pub id: u64 }
`

	os.MkdirAll(filepath.Join(tempDir, "rust"), 0755)
	os.MkdirAll(filepath.Join(existingDir, "rust"), 0755)
	os.WriteFile(filepath.Join(tempDir, "rust", "user.rs"), []byte(rustContent1), 0644)
	os.WriteFile(filepath.Join(existingDir, "rust", "user.rs"), []byte(rustContent2), 0644)

	// Should detect functional change
	diffs := compareDirectory(
		filepath.Join(tempDir, "rust"),
		filepath.Join(existingDir, "rust"),
		true,
	)

	if len(diffs) != 1 {
		t.Errorf("Expected 1 difference for functional change, got: %v", diffs)
	}
}
