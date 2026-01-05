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

// TestGoplsServiceIntegration verifies the gopls service can be created and initialized
func TestGoplsServiceIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Skip if gopls is not available
	if _, err := exec.LookPath("gopls"); err != nil {
		t.Skip("gopls not available in PATH - install with: go install golang.org/x/tools/gopls@latest")
	}

	// Create a temporary Go workspace
	tmpDir := t.TempDir()

	// Create a simple go.mod
	goMod := []byte("module test\n\ngo 1.21\n")
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), goMod, 0644); err != nil {
		t.Fatalf("Failed to create go.mod: %v", err)
	}

	// Create a simple Go file
	goFile := []byte(`package main

import "fmt"

func greet(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}

func main() {
	message := greet("World")
	fmt.Println(message)
}
`)
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), goFile, 0644); err != nil {
		t.Fatalf("Failed to create main.go: %v", err)
	}

	// Create gopls service
	service, err := gopls.NewService(gopls.Config{
		WorkspaceRoot: tmpDir,
		Logger:        logger.Logger,
	})
	if err != nil {
		t.Fatalf("Failed to create gopls service: %v", err)
	}

	// Initialize the service
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := service.Initialize(ctx); err != nil {
		t.Fatalf("Failed to initialize gopls service: %v", err)
	}

	// Verify service is initialized
	if !service.IsInitialized() {
		t.Fatal("Service should be initialized")
	}

	// Wait for gopls to analyze the workspace
	time.Sleep(2 * time.Second)

	// Test go-to-definition
	uri := "file://" + filepath.Join(tmpDir, "main.go")
	// Position of "greet" in the main function (line 9, character 15)
	pos := gopls.Position{Line: 9, Character: 15}

	locs, err := service.GoToDefinition(ctx, uri, pos)
	if err != nil {
		t.Fatalf("GoToDefinition failed: %v", err)
	}

	if len(locs) == 0 {
		t.Fatal("Expected at least one definition location")
	}

	t.Logf("✓ Go-to-definition found %d location(s)", len(locs))

	// Test hover
	hover, err := service.GetHover(ctx, uri, pos)
	if err != nil {
		t.Fatalf("GetHover failed: %v", err)
	}

	if hover == nil {
		t.Fatal("Expected hover information")
	}

	t.Logf("✓ Hover returned: %s", hover.Contents)

	// Shutdown the service
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := service.Shutdown(shutdownCtx); err != nil {
		// "client is shutdown" error is acceptable - means already cleaned up
		if !strings.Contains(err.Error(), "client is shutdown") {
			t.Fatalf("Failed to shutdown gopls service: %v", err)
		}
		t.Logf("⚠️  Shutdown returned expected error (client already shutdown): %v", err)
	}

	t.Log("✓ gopls service integration test passed")
}
