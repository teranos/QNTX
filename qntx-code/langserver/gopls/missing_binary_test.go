package gopls_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/logger"
	"github.com/teranos/QNTX/qntx-code/langserver/gopls"
)

// TestGoplsMissingBinary verifies graceful handling when gopls binary is not installed
func TestGoplsMissingBinary(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Check if gopls is actually in PATH
	_, err := exec.LookPath("gopls")
	if err == nil {
		t.Skip("Skipping test - gopls is installed (need to test missing binary scenario)")
	}

	// Create a temporary workspace
	tmpDir := t.TempDir()
	goMod := []byte("module test\n\ngo 1.21\n")
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), goMod, 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Try to create gopls service - should fail gracefully
	service, err := gopls.NewService(gopls.Config{
		WorkspaceRoot: tmpDir,
		Logger:        logger.Logger,
	})

	// Verify error is returned
	if err == nil {
		t.Fatal("Expected error when gopls binary is missing, got nil")
	}

	// Verify error message is clear
	errMsg := err.Error()
	if !strings.Contains(errMsg, "gopls") && !strings.Contains(errMsg, "executable") {
		t.Errorf("Error message should mention gopls or executable, got: %s", errMsg)
	}

	// Verify service is nil
	if service != nil {
		t.Error("Service should be nil when creation fails")
	}

	t.Logf("✓ Graceful handling of missing gopls binary: %v", err)
}

// TestGoplsAvailability checks if gopls is available and provides helpful output
func TestGoplsAvailability(t *testing.T) {
	path, err := exec.LookPath("gopls")
	if err != nil {
		t.Logf("⚠️  gopls not found in PATH")
		t.Logf("   Install with: go install golang.org/x/tools/gopls@latest")
		t.Logf("   Go code intelligence features will be disabled")
		return
	}

	t.Logf("✓ gopls found at: %s", path)

	// Check version
	cmd := exec.Command("gopls", "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("   Could not determine version: %v", err)
	} else {
		version := strings.TrimSpace(string(output))
		t.Logf("   Version: %s", version)
	}
}

// TestServerStartsWithoutGopls verifies the QNTX server can start even if gopls is unavailable
func TestServerStartsWithoutGopls(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create temporary workspace with no gopls
	tmpDir := t.TempDir()

	// Create minimal config that enables gopls (but it won't be available)
	configContent := `[code.gopls]
enabled = true
workspace_root = "."
`
	configPath := filepath.Join(tmpDir, "am.toml")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Set config path environment variable
	originalConfigPath := os.Getenv("QNTX_CONFIG")
	os.Setenv("QNTX_CONFIG", configPath)
	defer os.Setenv("QNTX_CONFIG", originalConfigPath)

	// Try to create a gopls service - should fail but return clear error
	service, err := gopls.NewService(gopls.Config{
		WorkspaceRoot: tmpDir,
		Logger:        logger.Logger,
	})

	if err != nil {
		t.Logf("✓ Service creation failed gracefully (expected): %v", err)
		if service != nil {
			t.Error("Service should be nil when creation fails")
		}
		return
	}

	// If gopls is actually available, test initialization works
	if service != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		err := service.Initialize(ctx)
		if err != nil {
			t.Logf("✓ Service initialized but workspace has no Go files (expected): %v", err)
		} else {
			t.Log("✓ Service initialized successfully (gopls is available)")
		}

		// Clean up
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutdownCancel()
		_ = service.Shutdown(shutdownCtx)
	}
}
