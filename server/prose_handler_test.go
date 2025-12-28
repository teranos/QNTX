package server

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Test validateProsePath with various inputs
func TestValidateProsePath(t *testing.T) {
	db := createTestDB(t)
	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	tests := []struct {
		name        string
		urlPath     string
		wantPath    string
		wantErr     bool
		errContains string
	}{
		{
			name:     "valid markdown file",
			urlPath:  "/api/prose/test.md",
			wantPath: "test.md",
			wantErr:  false,
		},
		{
			name:     "valid nested markdown file",
			urlPath:  "/api/prose/docs/guide.md",
			wantPath: "docs/guide.md",
			wantErr:  false,
		},
		{
			name:     "empty path defaults to index",
			urlPath:  "/api/prose/",
			wantPath: "index.md",
			wantErr:  false,
		},
		{
			name:        "directory traversal with dots",
			urlPath:     "/api/prose/../../../etc/passwd.md",
			wantErr:     true,
			errContains: "path traversal",
		},
		{
			name:        "absolute path",
			urlPath:     "/api/prose//etc/passwd.md",
			wantErr:     true,
			errContains: "path traversal",
		},
		{
			name:        "non-markdown file",
			urlPath:     "/api/prose/script.js",
			wantErr:     true,
			errContains: "only markdown files",
		},
		{
			name:        "html file attempt",
			urlPath:     "/api/prose/evil.html",
			wantErr:     true,
			errContains: "only markdown files",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, err := srv.validateProsePath(tt.urlPath, "test-client")

			if tt.wantErr {
				if err == nil {
					t.Errorf("validateProsePath() expected error, got nil")
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("validateProsePath() error = %v, want error containing %q", err, tt.errContains)
				}
				return
			}

			if err != nil {
				t.Errorf("validateProsePath() unexpected error: %v", err)
				return
			}

			if gotPath != tt.wantPath {
				t.Errorf("validateProsePath() = %q, want %q", gotPath, tt.wantPath)
			}
		})
	}
}

// Test readRequestBody with size limits
func TestReadRequestBody(t *testing.T) {
	db := createTestDB(t)
	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	t.Run("small body", func(t *testing.T) {
		content := []byte("# Test Document\n\nThis is a test.")
		body := io.NopCloser(bytes.NewReader(content))

		result, err := srv.readRequestBody(body)
		if err != nil {
			t.Errorf("readRequestBody() unexpected error: %v", err)
		}

		if !bytes.Equal(result, content) {
			t.Errorf("readRequestBody() = %q, want %q", result, content)
		}
	})

	t.Run("body at size limit", func(t *testing.T) {
		// 10MB exactly
		size := 10 * 1024 * 1024
		content := bytes.Repeat([]byte("x"), size)
		body := io.NopCloser(bytes.NewReader(content))

		result, err := srv.readRequestBody(body)
		if err != nil {
			t.Errorf("readRequestBody() unexpected error: %v", err)
		}

		if len(result) != size {
			t.Errorf("readRequestBody() length = %d, want %d", len(result), size)
		}
	})

	t.Run("body exceeds size limit", func(t *testing.T) {
		// 11MB (exceeds 10MB limit)
		size := 11 * 1024 * 1024
		content := bytes.Repeat([]byte("x"), size)
		body := io.NopCloser(bytes.NewReader(content))

		result, err := srv.readRequestBody(body)
		if err != nil {
			t.Errorf("readRequestBody() unexpected error: %v", err)
		}

		// Should be truncated to 10MB
		if len(result) != 10*1024*1024 {
			t.Errorf("readRequestBody() length = %d, want %d (truncated)", len(result), 10*1024*1024)
		}
	})
}

// Test writeProseFile
func TestWriteProseFile(t *testing.T) {
	db := createTestDB(t)
	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Create temp directory for testing
	tempDir := t.TempDir()
	originalWd, _ := os.Getwd()
	defer os.Chdir(originalWd)

	// Change to temp directory so "docs/" is created there
	os.Chdir(tempDir)

	t.Run("write simple file", func(t *testing.T) {
		content := []byte("# Test\n\nContent")
		err := srv.writeProseFile("test.md", content)
		if err != nil {
			t.Errorf("writeProseFile() unexpected error: %v", err)
		}

		// Verify file was written
		fullPath := filepath.Join("docs", "test.md")
		readContent, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("Failed to read written file: %v", err)
		}

		if !bytes.Equal(readContent, content) {
			t.Errorf("File content = %q, want %q", readContent, content)
		}
	})

	t.Run("write nested file creates directories", func(t *testing.T) {
		content := []byte("# Nested\n\nContent")
		err := srv.writeProseFile("subdir/nested.md", content)
		if err != nil {
			t.Errorf("writeProseFile() unexpected error: %v", err)
		}

		// Verify file and directory were created
		fullPath := filepath.Join("docs", "subdir", "nested.md")
		readContent, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("Failed to read written nested file: %v", err)
		}

		if !bytes.Equal(readContent, content) {
			t.Errorf("File content = %q, want %q", readContent, content)
		}
	})

	t.Run("overwrite existing file", func(t *testing.T) {
		// Write initial content
		initialContent := []byte("# Initial")
		err := srv.writeProseFile("overwrite.md", initialContent)
		if err != nil {
			t.Errorf("writeProseFile() initial write error: %v", err)
		}

		// Overwrite with new content
		newContent := []byte("# Updated")
		err = srv.writeProseFile("overwrite.md", newContent)
		if err != nil {
			t.Errorf("writeProseFile() overwrite error: %v", err)
		}

		// Verify new content
		fullPath := filepath.Join("docs", "overwrite.md")
		readContent, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("Failed to read overwritten file: %v", err)
		}

		if !bytes.Equal(readContent, newContent) {
			t.Errorf("File content = %q, want %q", readContent, newContent)
		}
	})
}
