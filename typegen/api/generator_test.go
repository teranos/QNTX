package api

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseRouting(t *testing.T) {
	// Create a temp directory with test routing.go
	tmpDir := t.TempDir()
	routingContent := `package server

import "net/http"

func (s *QNTXServer) setupHTTPRoutes() {
	http.HandleFunc("/health", s.corsMiddleware(s.HandleHealth))
	http.HandleFunc("/api/config", s.corsMiddleware(s.HandleConfig))
	http.HandleFunc("/api/pulse/schedules", s.corsMiddleware(s.HandlePulseSchedules))
	http.HandleFunc("/ws", s.corsMiddleware(s.HandleWebSocket))
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "routing.go"), []byte(routingContent), 0644); err != nil {
		t.Fatal(err)
	}

	gen := NewGenerator(tmpDir, "")
	if err := gen.parseRouting(); err != nil {
		t.Fatalf("parseRouting failed: %v", err)
	}

	if len(gen.endpoints) != 4 {
		t.Errorf("expected 4 endpoints, got %d", len(gen.endpoints))
	}

	// Check specific endpoints
	foundHealth := false
	foundSchedules := false
	for _, ep := range gen.endpoints {
		if ep.Pattern == "/health" && ep.Handler == "HandleHealth" {
			foundHealth = true
		}
		if ep.Pattern == "/api/pulse/schedules" && ep.Handler == "HandlePulseSchedules" {
			foundSchedules = true
		}
	}

	if !foundHealth {
		t.Error("expected to find /health endpoint")
	}
	if !foundSchedules {
		t.Error("expected to find /api/pulse/schedules endpoint")
	}
}

func TestParseProto(t *testing.T) {
	tmpDir := t.TempDir()
	protoContent := `syntax = "proto3";

package protocol;

// DomainPluginService is the gRPC service
service DomainPluginService {
  // Metadata returns plugin metadata
  rpc Metadata(Empty) returns (MetadataResponse);

  // HandleHTTP handles an HTTP request
  rpc HandleHTTP(HTTPRequest) returns (HTTPResponse);

  // HandleWebSocket handles bidirectional streaming
  rpc HandleWebSocket(stream WebSocketMessage) returns (stream WebSocketMessage);
}

message Empty {}

message MetadataResponse {
  string name = 1;
  string version = 2;
}

message HTTPRequest {
  string method = 1;
  string path = 2;
}
`
	if err := os.WriteFile(filepath.Join(tmpDir, "domain.proto"), []byte(protoContent), 0644); err != nil {
		t.Fatal(err)
	}

	gen := NewGenerator("", tmpDir)
	if err := gen.parseProto(); err != nil {
		t.Fatalf("parseProto failed: %v", err)
	}

	if len(gen.grpcMethods) != 3 {
		t.Errorf("expected 3 gRPC methods, got %d", len(gen.grpcMethods))
	}

	// Check HandleWebSocket is bidirectional
	for _, m := range gen.grpcMethods {
		if m.Name == "HandleWebSocket" {
			if m.Streaming != "bidi" {
				t.Errorf("expected HandleWebSocket to be bidi streaming, got %q", m.Streaming)
			}
		}
	}

	if len(gen.protoMessages) < 3 {
		t.Errorf("expected at least 3 proto messages, got %d", len(gen.protoMessages))
	}
}

func TestGenerateIndex(t *testing.T) {
	gen := &Generator{
		endpoints: []Endpoint{
			{Pattern: "/health", Methods: []string{"GET"}, Handler: "HandleHealth"},
			{Pattern: "/api/config", Methods: []string{"GET", "POST"}, Handler: "HandleConfig"},
		},
		grpcMethods: []GRPCMethod{
			{Name: "Metadata", Request: "Empty", Response: "MetadataResponse"},
		},
		wsMessageTypes: []WebSocketMessageType{
			{Type: "query", Direction: "client→server"},
		},
	}

	categories := gen.groupByCategory()
	index := gen.generateIndex(categories)

	// Check index contains expected sections
	if !strings.Contains(index, "REST API") {
		t.Error("index should contain REST API section")
	}
	if !strings.Contains(index, "WebSocket Protocol") {
		t.Error("index should contain WebSocket Protocol section")
	}
	if !strings.Contains(index, "Plugin gRPC API") {
		t.Error("index should contain Plugin gRPC API section")
	}
}

func TestCategoryToFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Health & Status", "health-status.md"},
		{"Pulse Schedules", "pulse-schedules.md"},
		{"Prose (Documents)", "prose-documents.md"},
	}

	for _, tt := range tests {
		got := categoryToFilename(tt.input)
		if got != tt.expected {
			t.Errorf("categoryToFilename(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestInferMethods(t *testing.T) {
	tests := []struct {
		handler  string
		pattern  string
		expected []string
	}{
		{"HandleHealth", "/health", []string{"GET"}},
		{"HandlePulseSchedules", "/api/pulse/schedules", []string{"GET", "POST"}},
		{"HandleWebSocket", "/ws", []string{"WS"}},
		{"HandleConfig", "/api/config", []string{"GET", "POST", "PATCH"}},
	}

	for _, tt := range tests {
		got := inferMethods(tt.handler, tt.pattern)
		if len(got) != len(tt.expected) {
			t.Errorf("inferMethods(%q, %q) = %v, want %v", tt.handler, tt.pattern, got, tt.expected)
		}
	}
}

func TestGenerateAPIDoc_Integration(t *testing.T) {
	// This test runs against the actual server and proto directories
	// Skip if the directories don't exist (e.g., running in CI without full repo)
	if _, err := os.Stat("../../server/routing.go"); os.IsNotExist(err) {
		t.Skip("server/routing.go not found, skipping integration test")
	}

	tmpDir := t.TempDir()

	err := GenerateAPIDoc("../../server", tmpDir)
	if err != nil {
		t.Fatalf("GenerateAPIDoc failed: %v", err)
	}

	// Check that expected files were created
	expectedFiles := []string{
		"README.md",
		"health-status.md",
		"websocket.md",
	}

	for _, file := range expectedFiles {
		path := filepath.Join(tmpDir, file)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %s to be created", file)
		}
	}

	// Read and verify README content
	readme, err := os.ReadFile(filepath.Join(tmpDir, "README.md"))
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(readme), "QNTX Server API Reference") {
		t.Error("README should contain title")
	}
	if !strings.Contains(string(readme), "REST API") {
		t.Error("README should contain REST API section")
	}
}
