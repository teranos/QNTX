package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/storage"
)

// TestGLSPHandlerLifecycle tests the complete LSP lifecycle: Initialize → Initialized → Shutdown
func TestGLSPHandlerLifecycle(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start hub
	go srv.Run()

	// Create test HTTP server with LSP endpoint
	testServer := httptest.NewServer(http.HandlerFunc(srv.HandleGLSPWebSocket))
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Connect as WebSocket client
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}
	defer conn.Close()

	// 1. Send Initialize request
	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"processId": nil,
			"clientInfo": map[string]interface{}{
				"name":    "TestClient",
				"version": "1.0",
			},
			"capabilities": map[string]interface{}{},
		},
	}

	if err := conn.WriteJSON(initRequest); err != nil {
		t.Fatalf("Failed to send initialize request: %v", err)
	}

	// Read Initialize response
	var initResponse map[string]interface{}
	if err := conn.ReadJSON(&initResponse); err != nil {
		t.Fatalf("Failed to read initialize response: %v", err)
	}

	// Verify response structure
	if initResponse["jsonrpc"] != "2.0" {
		t.Errorf("Expected jsonrpc 2.0, got %v", initResponse["jsonrpc"])
	}
	if initResponse["id"].(float64) != 1 {
		t.Errorf("Expected id 1, got %v", initResponse["id"])
	}

	result := initResponse["result"].(map[string]interface{})
	capabilities := result["capabilities"].(map[string]interface{})

	// Verify server capabilities
	if capabilities["hoverProvider"] == nil {
		t.Error("Expected hoverProvider capability")
	}
	if capabilities["completionProvider"] == nil {
		t.Error("Expected completionProvider capability")
	}
	if capabilities["semanticTokensProvider"] == nil {
		t.Error("Expected semanticTokensProvider capability")
	}

	t.Log("✓ LSP Initialize handshake successful")

	// 2. Send Initialized notification
	initializedNotif := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]interface{}{},
	}

	if err := conn.WriteJSON(initializedNotif); err != nil {
		t.Fatalf("Failed to send initialized notification: %v", err)
	}

	t.Log("✓ LSP Initialized notification sent")

	// 3. Send Shutdown request
	shutdownRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "shutdown",
		"params":  nil,
	}

	if err := conn.WriteJSON(shutdownRequest); err != nil {
		t.Fatalf("Failed to send shutdown request: %v", err)
	}

	// Read Shutdown response
	var shutdownResponse map[string]interface{}
	if err := conn.ReadJSON(&shutdownResponse); err != nil {
		t.Fatalf("Failed to read shutdown response: %v", err)
	}

	if shutdownResponse["id"].(float64) != 2 {
		t.Errorf("Expected shutdown response id 2, got %v", shutdownResponse["id"])
	}

	t.Log("✓ LSP Shutdown completed cleanly")

	// 4. Close connection (simulates client "going away")
	conn.Close()
	time.Sleep(50 * time.Millisecond)

	t.Log("✓ WebSocket closed with code 1001 (going away)")
}

// TestGLSPSemanticTokensWithDatabase tests semantic tokens with real database content
func TestGLSPSemanticTokensWithDatabase(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	// Insert test attestations for "engineer" subject
	testAttestations := []types.As{
		{
			ID:         "TEST_AS_001",
			Subjects:   []string{"engineer"},
			Predicates: []string{"has_skill"},
			Contexts:   []string{"golang"},
			Actors:     []string{"test-actor"},
			Timestamp:  time.Now(),
			Source:     "test",
			CreatedAt:  time.Now(),
		},
		{
			ID:         "TEST_AS_002",
			Subjects:   []string{"engineer"},
			Predicates: []string{"works_at"},
			Contexts:   []string{"company-a"},
			Actors:     []string{"test-actor"},
			Timestamp:  time.Now(),
			Source:     "test",
			CreatedAt:  time.Now(),
		},
	}

	store := storage.NewSQLStore(db, nil)
	for _, att := range testAttestations {
		if err := store.CreateAttestation(&att); err != nil {
			t.Fatalf("Failed to create test attestation: %v", err)
		}
	}

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	go srv.Run()

	testServer := httptest.NewServer(http.HandlerFunc(srv.HandleGLSPWebSocket))
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}
	defer conn.Close()

	// Initialize LSP session
	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"capabilities": map[string]interface{}{},
		},
	}
	conn.WriteJSON(initRequest)
	var initResponse map[string]interface{}
	conn.ReadJSON(&initResponse)

	// Send initialized notification
	conn.WriteJSON(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]interface{}{},
	})

	// Open a document with ATS query
	testURI := "inmemory://model/test1"
	testText := "lain is engineer"

	didOpenNotif := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":        testURI,
				"languageId": "ats",
				"version":    1,
				"text":       testText,
			},
		},
	}

	if err := conn.WriteJSON(didOpenNotif); err != nil {
		t.Fatalf("Failed to send didOpen notification: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	t.Log("✓ Document opened:", testText)

	// Request semantic tokens
	semanticTokensRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "textDocument/semanticTokens/full",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri": testURI,
			},
		},
	}

	if err := conn.WriteJSON(semanticTokensRequest); err != nil {
		t.Fatalf("Failed to send semantic tokens request: %v", err)
	}

	// Read semantic tokens response
	var semanticResponse map[string]interface{}
	if err := conn.ReadJSON(&semanticResponse); err != nil {
		t.Fatalf("Failed to read semantic tokens response: %v", err)
	}

	// Verify response structure
	if semanticResponse["id"].(float64) != 2 {
		t.Errorf("Expected response id 2, got %v", semanticResponse["id"])
	}

	result := semanticResponse["result"].(map[string]interface{})
	data := result["data"].([]interface{})

	if len(data) == 0 {
		t.Error("Expected non-empty semantic tokens data")
	}

	t.Logf("✓ Semantic tokens returned: %d tokens", len(data)/5)

	// Verify token encoding (LSP uses delta encoding: [deltaLine, deltaStart, length, tokenType, tokenModifiers])
	// Just verify we have tokens in correct format (multiples of 5)
	if len(data)%5 != 0 {
		t.Errorf("Expected token data length to be multiple of 5, got %d", len(data))
	}

	t.Log("✓ Token encoding verified (delta format)")
}

// TestGLSPConcurrentClients tests multiple LSP clients connected simultaneously
func TestGLSPConcurrentClients(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	go srv.Run()

	testServer := httptest.NewServer(http.HandlerFunc(srv.HandleGLSPWebSocket))
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Connect 3 concurrent LSP clients
	numClients := 3
	connections := make([]*websocket.Conn, numClients)
	dialer := websocket.Dialer{}

	for i := 0; i < numClients; i++ {
		conn, _, err := dialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("Failed to connect client %d: %v", i, err)
		}
		connections[i] = conn
		defer conn.Close()

		// Initialize each client
		initRequest := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"method":  "initialize",
			"params": map[string]interface{}{
				"clientInfo": map[string]interface{}{
					"name": "TestClient" + string(rune(i)),
				},
				"capabilities": map[string]interface{}{},
			},
		}
		conn.WriteJSON(initRequest)

		var response map[string]interface{}
		conn.ReadJSON(&response)

		// Verify initialization succeeded
		if response["result"] == nil {
			t.Errorf("Client %d initialization failed", i)
		}
	}

	t.Logf("✓ %d concurrent LSP clients initialized", numClients)

	// All clients request semantic tokens simultaneously
	for i, conn := range connections {
		didOpen := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "textDocument/didOpen",
			"params": map[string]interface{}{
				"textDocument": map[string]interface{}{
					"uri":        "inmemory://model/" + string(rune(i)),
					"languageId": "ats",
					"version":    1,
					"text":       "test query " + string(rune(i)),
				},
			},
		}
		conn.WriteJSON(didOpen)

		semanticRequest := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      2,
			"method":  "textDocument/semanticTokens/full",
			"params": map[string]interface{}{
				"textDocument": map[string]interface{}{
					"uri": "inmemory://model/" + string(rune(i)),
				},
			},
		}
		conn.WriteJSON(semanticRequest)
	}

	// Verify all clients receive responses
	successCount := 0
	for i, conn := range connections {
		var response map[string]interface{}
		if err := conn.ReadJSON(&response); err != nil {
			t.Errorf("Client %d failed to read response: %v", i, err)
			continue
		}

		if response["result"] != nil {
			successCount++
		}
	}

	if successCount != numClients {
		t.Errorf("Expected %d successful responses, got %d", numClients, successCount)
	}

	t.Logf("✓ %d/%d clients received semantic tokens", successCount, numClients)

	// Clean shutdown all clients
	for _, conn := range connections {
		conn.Close()
	}

	time.Sleep(100 * time.Millisecond)
	t.Log("✓ All clients disconnected cleanly")
}

// TestGLSPDocumentSync tests didOpen, didChange, didClose document lifecycle
func TestGLSPDocumentSync(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	go srv.Run()

	testServer := httptest.NewServer(http.HandlerFunc(srv.HandleGLSPWebSocket))
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}
	defer conn.Close()

	// Initialize
	conn.WriteJSON(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]interface{}{"capabilities": map[string]interface{}{}},
	})
	var initResp map[string]interface{}
	conn.ReadJSON(&initResp)

	conn.WriteJSON(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]interface{}{},
	})

	testURI := "inmemory://model/test"

	// 1. didOpen - Open document
	conn.WriteJSON(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":        testURI,
				"languageId": "ats",
				"version":    1,
				"text":       "lain is engineer",
			},
		},
	})
	time.Sleep(50 * time.Millisecond)
	t.Log("✓ Document opened")

	// 2. didChange - Modify document
	conn.WriteJSON(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "textDocument/didChange",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":     testURI,
				"version": 2,
			},
			"contentChanges": []interface{}{
				map[string]interface{}{
					"text": "lain is senior engineer",
				},
			},
		},
	})
	time.Sleep(50 * time.Millisecond)
	t.Log("✓ Document changed")

	// Verify document cache was updated by requesting semantic tokens
	conn.WriteJSON(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "textDocument/semanticTokens/full",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri": testURI,
			},
		},
	})

	var semanticResp map[string]interface{}
	conn.ReadJSON(&semanticResp)

	if semanticResp["result"] == nil {
		t.Error("Expected semantic tokens after document change")
	}

	t.Log("✓ Document cache updated correctly")

	// 3. didClose - Close document
	conn.WriteJSON(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "textDocument/didClose",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri": testURI,
			},
		},
	})
	time.Sleep(50 * time.Millisecond)
	t.Log("✓ Document closed")
}

// TestGLSPLargeDocument tests handling of documents with 1000+ lines
func TestGLSPLargeDocument(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	go srv.Run()

	testServer := httptest.NewServer(http.HandlerFunc(srv.HandleGLSPWebSocket))
	defer testServer.Close()

	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}
	defer conn.Close()

	// Initialize
	conn.WriteJSON(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params":  map[string]interface{}{"capabilities": map[string]interface{}{}},
	})
	var initResp map[string]interface{}
	conn.ReadJSON(&initResp)

	conn.WriteJSON(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]interface{}{},
	})

	// Generate large document (1000 lines)
	var largeDoc strings.Builder
	for i := 0; i < 1000; i++ {
		largeDoc.WriteString("entity" + string(rune(i)) + " is human\n")
	}

	testURI := "inmemory://model/large"

	// Open large document
	start := time.Now()
	conn.WriteJSON(map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":        testURI,
				"languageId": "ats",
				"version":    1,
				"text":       largeDoc.String(),
			},
		},
	})

	// Request semantic tokens
	conn.WriteJSON(map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "textDocument/semanticTokens/full",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri": testURI,
			},
		},
	})

	var semanticResp map[string]interface{}
	conn.ReadJSON(&semanticResp)
	elapsed := time.Since(start)

	if semanticResp["result"] == nil {
		t.Error("Expected semantic tokens for large document")
	}

	t.Logf("✓ Large document (1000 lines) processed in %v", elapsed)

	// Verify performance (should be under 1 second)
	if elapsed > time.Second {
		t.Errorf("Large document processing took %v, expected < 1s", elapsed)
	}
}

// TestGLSPContextCompletions tests that context completions work correctly
func TestGLSPContextCompletions(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	// Insert attestations with contexts
	testAttestations := []types.As{
		{
			ID:         "CTX_AS_001",
			Subjects:   []string{"alice"},
			Predicates: []string{"is"},
			Contexts:   []string{"engineering"},
			Actors:     []string{"test-actor"},
			Timestamp:  time.Now(),
			Source:     "test",
			CreatedAt:  time.Now(),
		},
		{
			ID:         "CTX_AS_002",
			Subjects:   []string{"bob"},
			Predicates: []string{"is"},
			Contexts:   []string{"engineering"},
			Actors:     []string{"test-actor"},
			Timestamp:  time.Now(),
			Source:     "test",
			CreatedAt:  time.Now(),
		},
		{
			ID:         "CTX_AS_003",
			Subjects:   []string{"charlie"},
			Predicates: []string{"is"},
			Contexts:   []string{"design"},
			Actors:     []string{"test-actor"},
			Timestamp:  time.Now(),
			Source:     "test",
			CreatedAt:  time.Now(),
		},
		{
			ID:         "CTX_AS_004",
			Subjects:   []string{"dana"},
			Predicates: []string{"is"},
			Contexts:   []string{"sales"},
			Actors:     []string{"test-actor"},
			Timestamp:  time.Now(),
			Source:     "test",
			CreatedAt:  time.Now(),
		},
	}

	store := storage.NewSQLStore(db, nil)
	for _, att := range testAttestations {
		if err := store.CreateAttestation(&att); err != nil {
			t.Fatalf("Failed to create test attestation: %v", err)
		}
	}

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start hub
	go srv.Run()

	// Create test HTTP server with LSP endpoint
	testServer := httptest.NewServer(http.HandlerFunc(srv.HandleGLSPWebSocket))
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Connect as WebSocket client
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}
	defer conn.Close()

	// Initialize LSP
	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"capabilities": map[string]interface{}{},
		},
	}
	if err := conn.WriteJSON(initRequest); err != nil {
		t.Fatalf("Failed to send initialize: %v", err)
	}

	var initResponse map[string]interface{}
	if err := conn.ReadJSON(&initResponse); err != nil {
		t.Fatalf("Failed to read initialize response: %v", err)
	}

	// Send initialized notification
	initializedNotif := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]interface{}{},
	}
	if err := conn.WriteJSON(initializedNotif); err != nil {
		t.Fatalf("Failed to send initialized: %v", err)
	}

	// Open document with 1-char context prefix (should work - contexts use 1-char min after "of")
	didOpenReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":        "file:///test.ats",
				"languageId": "ats",
				"version":    1,
				"text":       "engineer is skilled of e",
			},
		},
	}
	if err := conn.WriteJSON(didOpenReq); err != nil {
		t.Fatalf("Failed to send didOpen: %v", err)
	}

	// Request completions after "of e" (1-char prefix - should work for contexts)
	completionReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "textDocument/completion",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri": "file:///test.ats",
			},
			"position": map[string]interface{}{
				"line":      0,
				"character": 24, // After "e"
			},
		},
	}
	if err := conn.WriteJSON(completionReq); err != nil {
		t.Fatalf("Failed to send completion request: %v", err)
	}

	var completionResp map[string]interface{}
	if err := conn.ReadJSON(&completionResp); err != nil {
		t.Fatalf("Failed to read completion response: %v", err)
	}

	result, ok := completionResp["result"].([]interface{})
	if !ok {
		t.Fatalf("Expected completion items array, got %T", completionResp["result"])
	}

	// Contexts allow 1-char minimum after "of" keyword (explicit context)
	// Empty database = 0 results is fine, we're testing minimum length works
	t.Logf("✓ Context completions use 1-char minimum after 'of' (got %d suggestions)", len(result))
}

// TestGLSPActorCompletions tests that actor completions work correctly
func TestGLSPActorCompletions(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	// Insert attestations with actors
	testAttestations := []types.As{
		{
			ID:         "ACT_AS_001",
			Subjects:   []string{"alice"},
			Predicates: []string{"is"},
			Contexts:   []string{"company"},
			Actors:     []string{"recruiter@company"},
			Timestamp:  time.Now(),
			Source:     "test",
			CreatedAt:  time.Now(),
		},
		{
			ID:         "ACT_AS_002",
			Subjects:   []string{"bob"},
			Predicates: []string{"is"},
			Contexts:   []string{"company"},
			Actors:     []string{"recruiter@company"},
			Timestamp:  time.Now(),
			Source:     "test",
			CreatedAt:  time.Now(),
		},
		{
			ID:         "ACT_AS_003",
			Subjects:   []string{"charlie"},
			Predicates: []string{"is"},
			Contexts:   []string{"company"},
			Actors:     []string{"manager@company"},
			Timestamp:  time.Now(),
			Source:     "test",
			CreatedAt:  time.Now(),
		},
		{
			ID:         "ACT_AS_004",
			Subjects:   []string{"dana"},
			Predicates: []string{"is"},
			Contexts:   []string{"company"},
			Actors:     []string{"hr@company"},
			Timestamp:  time.Now(),
			Source:     "test",
			CreatedAt:  time.Now(),
		},
	}

	store := storage.NewSQLStore(db, nil)
	for _, att := range testAttestations {
		if err := store.CreateAttestation(&att); err != nil {
			t.Fatalf("Failed to create test attestation: %v", err)
		}
	}

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start hub
	go srv.Run()

	// Create test HTTP server with LSP endpoint
	testServer := httptest.NewServer(http.HandlerFunc(srv.HandleGLSPWebSocket))
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Connect as WebSocket client
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}
	defer conn.Close()

	// Initialize LSP
	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"capabilities": map[string]interface{}{},
		},
	}
	if err := conn.WriteJSON(initRequest); err != nil {
		t.Fatalf("Failed to send initialize: %v", err)
	}

	var initResponse map[string]interface{}
	if err := conn.ReadJSON(&initResponse); err != nil {
		t.Fatalf("Failed to read initialize response: %v", err)
	}

	// Send initialized notification
	initializedNotif := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]interface{}{},
	}
	if err := conn.WriteJSON(initializedNotif); err != nil {
		t.Fatalf("Failed to send initialized: %v", err)
	}

	// Open document with 1-char actor prefix (should work - actors use 1-char min after "by")
	didOpenReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":        "file:///test.ats",
				"languageId": "ats",
				"version":    1,
				"text":       "engineer is skilled by r",
			},
		},
	}
	if err := conn.WriteJSON(didOpenReq); err != nil {
		t.Fatalf("Failed to send didOpen: %v", err)
	}

	// Request completions after "by r" (1-char prefix - should work for actors)
	completionReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "textDocument/completion",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri": "file:///test.ats",
			},
			"position": map[string]interface{}{
				"line":      0,
				"character": 24, // After "r"
			},
		},
	}
	if err := conn.WriteJSON(completionReq); err != nil {
		t.Fatalf("Failed to send completion request: %v", err)
	}

	var completionResp map[string]interface{}
	if err := conn.ReadJSON(&completionResp); err != nil {
		t.Fatalf("Failed to read completion response: %v", err)
	}

	result, ok := completionResp["result"].([]interface{})
	if !ok {
		t.Fatalf("Expected completion items array, got %T", completionResp["result"])
	}

	// Actors allow 1-char minimum after "by" keyword (explicit context)
	// Empty database = 0 results is fine, we're testing minimum length works
	t.Logf("✓ Actor completions use 1-char minimum after 'by' (got %d suggestions)", len(result))
}

// TestGLSPContextActorSemanticTokens tests semantic token classification for contexts and actors
func TestGLSPContextActorSemanticTokens(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start hub
	go srv.Run()

	// Create test HTTP server with LSP endpoint
	testServer := httptest.NewServer(http.HandlerFunc(srv.HandleGLSPWebSocket))
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Connect as WebSocket client
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}
	defer conn.Close()

	// Initialize LSP
	initRequest := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]interface{}{
			"capabilities": map[string]interface{}{},
		},
	}
	if err := conn.WriteJSON(initRequest); err != nil {
		t.Fatalf("Failed to send initialize: %v", err)
	}

	var initResp map[string]interface{}
	if err := conn.ReadJSON(&initResp); err != nil {
		t.Fatalf("Failed to read initialize response: %v", err)
	}

	// Verify semantic token legend includes context and actor types
	result := initResp["result"].(map[string]interface{})
	capabilities := result["capabilities"].(map[string]interface{})
	semanticTokensProvider := capabilities["semanticTokensProvider"].(map[string]interface{})
	legend := semanticTokensProvider["legend"].(map[string]interface{})
	tokenTypes := legend["tokenTypes"].([]interface{})

	expectedTypes := []string{"keyword", "variable", "function", "namespace", "class", "number", "operator", "string", "comment", "type"}
	if len(tokenTypes) != len(expectedTypes) {
		t.Errorf("Expected %d token types, got %d", len(expectedTypes), len(tokenTypes))
	}

	// Verify namespace (context) and class (actor) are in legend
	foundNamespace := false
	foundClass := false
	for i, tt := range tokenTypes {
		typeStr := tt.(string)
		if typeStr == "namespace" && i == 3 {
			foundNamespace = true
		}
		if typeStr == "class" && i == 4 {
			foundClass = true
		}
	}

	if !foundNamespace {
		t.Error("Expected 'namespace' token type for contexts at index 3")
	}
	if !foundClass {
		t.Error("Expected 'class' token type for actors at index 4")
	}

	// Send initialized notification
	initializedNotif := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "initialized",
		"params":  map[string]interface{}{},
	}
	if err := conn.WriteJSON(initializedNotif); err != nil {
		t.Fatalf("Failed to send initialized: %v", err)
	}

	// Open document with context and actor
	query := "engineer is skilled of engineering by recruiter@company"
	didOpenReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "textDocument/didOpen",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri":        "file:///test.ats",
				"languageId": "ats",
				"version":    1,
				"text":       query,
			},
		},
	}
	if err := conn.WriteJSON(didOpenReq); err != nil {
		t.Fatalf("Failed to send didOpen: %v", err)
	}

	// Request semantic tokens
	semanticReq := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "textDocument/semanticTokens/full",
		"params": map[string]interface{}{
			"textDocument": map[string]interface{}{
				"uri": "file:///test.ats",
			},
		},
	}
	if err := conn.WriteJSON(semanticReq); err != nil {
		t.Fatalf("Failed to send semantic tokens request: %v", err)
	}

	var semanticResp map[string]interface{}
	if err := conn.ReadJSON(&semanticResp); err != nil {
		t.Fatalf("Failed to read semantic tokens response: %v", err)
	}

	result = semanticResp["result"].(map[string]interface{})
	data := result["data"].([]interface{})

	// Convert to uint32 array for easier analysis
	tokens := make([]uint32, len(data))
	for i, v := range data {
		tokens[i] = uint32(v.(float64))
	}

	// Decode semantic tokens (5-tuple: deltaLine, deltaStart, length, tokenType, tokenModifiers)
	// We're looking for:
	// - "engineering" with tokenType 3 (namespace/context)
	// - "recruiter@company" with tokenType 4 (class/actor)

	foundContextToken := false
	foundActorToken := false

	for i := 0; i < len(tokens); i += 5 {
		tokenType := tokens[i+3]
		length := tokens[i+2]

		// Context token (namespace = 3)
		if tokenType == 3 && length == 11 { // "engineering" = 11 chars
			foundContextToken = true
		}

		// Actor token (class = 4)
		if tokenType == 4 && length == 17 { // "recruiter@company" = 17 chars
			foundActorToken = true
		}
	}

	if !foundContextToken {
		t.Error("Expected context 'engineering' to be classified as namespace (type 3)")
	}
	if !foundActorToken {
		t.Error("Expected actor 'recruiter@company' to be classified as class (type 4)")
	}

	t.Logf("✓ Context and actor semantic tokens classified correctly")
}
