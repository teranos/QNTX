package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	appcfg "github.com/teranos/QNTX/am"
)

func TestReadCodeFile_RejectsSymlinks(t *testing.T) {
	// Create temp workspace
	tmpDir := t.TempDir()

	// Change to temp directory so workspace detection finds it
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	// Override workspace root config for this test
	appcfg.GetViper().Set("code.gopls.workspace_root", tmpDir)

	// Create a real file
	realFile := filepath.Join(tmpDir, "real.go")
	if err := os.WriteFile(realFile, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("Failed to create real file: %v", err)
	}

	// Create symlink pointing to it
	symlinkPath := filepath.Join(tmpDir, "evil.go")
	if err := os.Symlink(realFile, symlinkPath); err != nil {
		t.Skipf("Cannot create symlink (maybe Windows without dev mode): %v", err)
	}

	// Create server
	db := createTestDB(t)
	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Try to read symlink through server
	_, err = srv.readCodeFile("evil.go")

	// Should reject with "symlinks not allowed"
	if err == nil {
		t.Fatal("expected error when reading symlink, got nil")
	}

	if !strings.Contains(err.Error(), "symlinks not allowed") {
		t.Errorf("expected 'symlinks not allowed' error, got: %v", err)
	}
}
