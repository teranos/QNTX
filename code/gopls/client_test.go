package gopls

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStdioClient_Initialize(t *testing.T) {
	// Create a temporary Go module for testing
	tmpDir := t.TempDir()

	// Create go.mod
	goMod := `module test
go 1.21
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("failed to create go.mod: %v", err)
	}

	// Create a simple Go file
	goFile := `package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(goFile), 0644); err != nil {
		t.Fatalf("failed to create main.go: %v", err)
	}

	// Create gopls client
	client, err := NewStdioClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Shutdown(context.Background())

	// Initialize with workspace
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := client.Initialize(ctx, tmpDir); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}
}

func TestStdioClient_GoToDefinition(t *testing.T) {
	// Create a temporary Go module for testing
	tmpDir := t.TempDir()

	// Create go.mod
	goMod := `module test
go 1.21
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("failed to create go.mod: %v", err)
	}

	// Create a Go file with a function definition
	goFile := `package main

import "fmt"

func greet(name string) {
	fmt.Println("Hello,", name)
}

func main() {
	greet("world")
}
`
	mainGoPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(mainGoPath, []byte(goFile), 0644); err != nil {
		t.Fatalf("failed to create main.go: %v", err)
	}

	// Create gopls client
	client, err := NewStdioClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Shutdown(context.Background())

	// Initialize
	ctx := context.Background()
	if err := client.Initialize(ctx, tmpDir); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	// Give gopls time to index the file
	time.Sleep(2 * time.Second)

	// Open the document so gopls can analyze it
	uri := "file://" + mainGoPath
	if err := client.notify("textDocument/didOpen", map[string]interface{}{
		"textDocument": map[string]interface{}{
			"uri":        uri,
			"languageId": "go",
			"version":    1,
			"text":       goFile,
		},
	}); err != nil {
		t.Fatalf("failed to open document: %v", err)
	}

	// Give gopls time to analyze the opened document
	time.Sleep(1 * time.Second)

	// Test: Go to definition of "greet" function call
	// Line 9 in the file (0-indexed, so 9 for "greet("world")")
	pos := Position{
		Line:      9,
		Character: 1, // Position of "greet" in main
	}

	locations, err := client.GoToDefinition(ctx, uri, pos)
	if err != nil {
		t.Fatalf("GoToDefinition failed: %v", err)
	}

	if len(locations) == 0 {
		t.Fatal("expected at least one location, got 0")
	}

	t.Logf("Found definition at: %s:%d:%d",
		locations[0].URI,
		locations[0].Range.Start.Line,
		locations[0].Range.Start.Character)
}

func TestStdioClient_GetHover(t *testing.T) {
	// Create a temporary Go module for testing
	tmpDir := t.TempDir()

	// Create go.mod
	goMod := `module test
go 1.21
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("failed to create go.mod: %v", err)
	}

	// Create a Go file
	goFile := `package main

import "fmt"

// Greet prints a greeting message
func Greet(name string) {
	fmt.Println("Hello,", name)
}
`
	mainGoPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(mainGoPath, []byte(goFile), 0644); err != nil {
		t.Fatalf("failed to create main.go: %v", err)
	}

	// Create gopls client
	client, err := NewStdioClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Shutdown(context.Background())

	// Initialize
	ctx := context.Background()
	if err := client.Initialize(ctx, tmpDir); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	// Give gopls time to index
	time.Sleep(2 * time.Second)

	// Test: Get hover info for Greet function
	uri := "file://" + mainGoPath
	pos := Position{
		Line:      5,
		Character: 6, // Position on "Greet"
	}

	hover, err := client.GetHover(ctx, uri, pos)
	if err != nil {
		t.Fatalf("GetHover failed: %v", err)
	}

	text := hover.GetText()
	if text == "" {
		t.Fatal("expected hover information, got none")
	}

	t.Logf("Hover contents: %s", text)
}

func TestStdioClient_ListDocumentSymbols(t *testing.T) {
	// Create a temporary Go module for testing
	tmpDir := t.TempDir()

	// Create go.mod
	goMod := `module test
go 1.21
`
	if err := os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644); err != nil {
		t.Fatalf("failed to create go.mod: %v", err)
	}

	// Create a Go file with multiple symbols
	goFile := `package main

const Version = "1.0.0"

type Config struct {
	Name string
}

func NewConfig() *Config {
	return &Config{}
}

func main() {
}
`
	mainGoPath := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(mainGoPath, []byte(goFile), 0644); err != nil {
		t.Fatalf("failed to create main.go: %v", err)
	}

	// Create gopls client
	client, err := NewStdioClient()
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Shutdown(context.Background())

	// Initialize
	ctx := context.Background()
	if err := client.Initialize(ctx, tmpDir); err != nil {
		t.Fatalf("failed to initialize: %v", err)
	}

	// Give gopls time to index
	time.Sleep(2 * time.Second)

	// Test: List document symbols
	uri := "file://" + mainGoPath
	symbols, err := client.ListDocumentSymbols(ctx, uri)
	if err != nil {
		t.Fatalf("ListDocumentSymbols failed: %v", err)
	}

	if len(symbols) == 0 {
		t.Fatal("expected symbols, got none")
	}

	t.Logf("Found %d symbols:", len(symbols))
	for _, sym := range symbols {
		t.Logf("  - %s (%s)", sym.Name, sym.Detail)
	}

	// Should find at least: Version, Config, NewConfig, main
	expectedSymbols := map[string]bool{
		"Version":   false,
		"Config":    false,
		"NewConfig": false,
		"main":      false,
	}

	for _, sym := range symbols {
		if _, ok := expectedSymbols[sym.Name]; ok {
			expectedSymbols[sym.Name] = true
		}
	}

	for name, found := range expectedSymbols {
		if !found {
			t.Errorf("expected to find symbol %q", name)
		}
	}
}
