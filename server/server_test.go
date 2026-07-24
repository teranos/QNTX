package server

import (
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	_ "github.com/mattn/go-sqlite3"
	"github.com/teranos/QNTX/ats"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// createTestDB is a local alias for qntxtest.CreateTestDB
func createTestDB(t *testing.T) *sql.DB {
	return qntxtest.CreateTestDB(t)
}

// createTestStore is a local alias for qntxtest.CreateTestStore
func createTestStore(t *testing.T) (ats.AttestationStore, *sql.DB) {
	return qntxtest.CreateTestStore(t)
}

// Test basic server creation and initialization
func TestNewQNTXServer(t *testing.T) {
	store, db := createTestStore(t)

	srv, err := NewQNTXServer(db, store, ":memory:", 1)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	if srv.db != db {
		t.Error("Server database not set correctly")
	}

	if int(srv.verbosity.Load()) != 1 {
		t.Errorf("Server verbosity = %d, want 1", int(srv.verbosity.Load()))
	}

	if srv.clients == nil {
		t.Error("Server clients map not initialized")
	}

}

// Test that the hub goroutine handles client registration
func TestServerHubRegistration(t *testing.T) {
	store, db := createTestStore(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, store, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start hub in background
	go srv.Run()

	// Create a mock client
	client := &Client{
		server:  srv,
		sendMsg: make(chan interface{}, 256),
		id:      "test_client_1",
	}

	// Register the client
	srv.register <- client

	// Give hub time to process
	time.Sleep(10 * time.Millisecond)

	// Verify client was registered
	srv.mu.RLock()
	_, exists := srv.clients[client]
	count := len(srv.clients)
	srv.mu.RUnlock()

	if !exists {
		t.Error("Client was not registered")
	}

	if count != 1 {
		t.Errorf("Server should have 1 client, got %d", count)
	}

}

// Test that the hub goroutine handles client unregistration
func TestServerHubUnregistration(t *testing.T) {
	store, db := createTestStore(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, store, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start hub in background
	go srv.Run()

	// Create and register a client
	client := &Client{
		server:  srv,
		sendMsg: make(chan interface{}, 256),
		id:      "test_client_unreg",
	}

	srv.register <- client
	time.Sleep(10 * time.Millisecond)

	// Verify registered
	srv.mu.RLock()
	_, exists := srv.clients[client]
	srv.mu.RUnlock()

	if !exists {
		t.Fatal("Client was not registered")
	}

	// Unregister the client
	srv.unregister <- client
	time.Sleep(10 * time.Millisecond)

	// Verify client was unregistered
	srv.mu.RLock()
	_, exists = srv.clients[client]
	count := len(srv.clients)
	srv.mu.RUnlock()

	if exists {
		t.Error("Client should have been unregistered")
	}

	if count != 0 {
		t.Errorf("Server should have 0 clients, got %d", count)
	}

}

// Test concurrent client registration
func TestServerConcurrentRegistration(t *testing.T) {
	store, db := createTestStore(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, store, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start hub
	go srv.Run()

	numClients := 20
	var wg sync.WaitGroup

	// Concurrently register many clients
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			client := &Client{
				server:  srv,
				sendMsg: make(chan interface{}, 256),
				id:      fmt.Sprintf("client_%d", id),
			}
			srv.register <- client
		}(i)
	}

	wg.Wait()

	// Give hub time to process all registrations
	time.Sleep(50 * time.Millisecond)

	// Verify all clients registered
	srv.mu.RLock()
	count := len(srv.clients)
	srv.mu.RUnlock()

	if count != numClients {
		t.Errorf("Expected %d clients, got %d", numClients, count)
	}
}

// Test port availability checking
func TestIsPortAvailable(t *testing.T) {
	// Port 0 should always be available (OS picks)
	if !isPortAvailable(0) {
		t.Error("Port 0 should be available")
	}

	// Very high port numbers should generally be available
	if !isPortAvailable(65432) {
		// This might fail on some systems, but is unlikely
		t.Log("Port 65432 not available (this may be environment-specific)")
	}
}

// Test WebSocket upgrade handler
func TestHandleWebSocket(t *testing.T) {
	store, db := createTestStore(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, store, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start hub
	go srv.Run()

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(srv.HandleWebSocket))
	defer testServer.Close()

	// Convert http:// to ws://
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	// Connect as WebSocket client
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, http.Header{"Origin": []string{"http://127.0.0.1"}})
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}
	defer conn.Close()

	// Give server time to register client
	time.Sleep(50 * time.Millisecond)

	// Verify client was registered
	srv.mu.RLock()
	clientCount := len(srv.clients)
	srv.mu.RUnlock()

	if clientCount != 1 {
		t.Errorf("Expected 1 client after WebSocket connection, got %d", clientCount)
	}

	// Close connection
	conn.Close()

	// Give server time to unregister client
	time.Sleep(50 * time.Millisecond)

	// Verify client was unregistered
	srv.mu.RLock()
	clientCount = len(srv.clients)
	srv.mu.RUnlock()

	if clientCount != 0 {
		t.Errorf("Expected 0 clients after WebSocket disconnect, got %d", clientCount)
	}
}

// Test query message handling
func TestHandleQueryMessage(t *testing.T) {
	store, db := createTestStore(t)
	defer db.Close()

	// Insert test data using production schema
	_, err := db.Exec(`
		INSERT INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at)
		VALUES ('ASTEST001', '["alice"]', '["is"]', '["TEST"]', '["system"]', datetime('now'), 'test', '{}', datetime('now'))
	`)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	srv, err := NewQNTXServer(db, store, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start hub
	go srv.Run()

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(srv.HandleWebSocket))
	defer testServer.Close()

	// Connect WebSocket client
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, http.Header{"Origin": []string{"http://127.0.0.1"}})
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}
	defer conn.Close()

	// Send query message
	queryMsg := map[string]interface{}{
		"type":  "query",
		"query": "is engineer",
	}

	err = conn.WriteJSON(queryMsg)
	if err != nil {
		t.Fatalf("Failed to send query: %v", err)
	}

	// Read response
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	var response map[string]interface{}
	err = conn.ReadJSON(&response)
	if err != nil {
		t.Fatalf("Failed to read response: %v", err)
	}

	// Verify response structure (should be a graph or log message)
	// The exact structure depends on the graph format, but we can check it's valid JSON
	if response == nil {
		t.Error("Response should not be nil")
	}

	t.Logf("Received response with keys: %v", getKeys(response))
}

// Test ping/pong message handling
func TestHandlePingMessage(t *testing.T) {
	store, db := createTestStore(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, store, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start hub
	go srv.Run()

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(srv.HandleWebSocket))
	defer testServer.Close()

	// Connect WebSocket client
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(wsURL, http.Header{"Origin": []string{"http://127.0.0.1"}})
	if err != nil {
		t.Fatalf("Failed to connect WebSocket: %v", err)
	}
	defer conn.Close()

	// Set pong handler
	pongReceived := make(chan bool, 1)
	conn.SetPongHandler(func(appData string) error {
		pongReceived <- true
		return nil
	})

	// Start reading messages (required for pong handler to be called)
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	// Send ping message (as JSON per protocol)
	pingMsg := map[string]interface{}{
		"type": "ping",
	}

	err = conn.WriteJSON(pingMsg)
	if err != nil {
		t.Fatalf("Failed to send ping: %v", err)
	}

	// The server will send ping messages via writePump ticker
	// We just verify the connection stays alive
	time.Sleep(100 * time.Millisecond)

	// Verify client is still registered
	srv.mu.RLock()
	clientCount := len(srv.clients)
	srv.mu.RUnlock()

	if clientCount != 1 {
		t.Error("Client should still be connected after ping")
	}
}

// Test multiple concurrent WebSocket clients
func TestMultipleWebSocketClients(t *testing.T) {
	store, db := createTestStore(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, store, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start hub
	go srv.Run()

	// Create test HTTP server
	testServer := httptest.NewServer(http.HandlerFunc(srv.HandleWebSocket))
	defer testServer.Close()

	// Connect multiple WebSocket clients
	numClients := 5
	connections := make([]*websocket.Conn, numClients)
	wsURL := "ws" + strings.TrimPrefix(testServer.URL, "http")

	for i := 0; i < numClients; i++ {
		dialer := websocket.Dialer{}
		conn, _, err := dialer.Dial(wsURL, http.Header{"Origin": []string{"http://127.0.0.1"}})
		if err != nil {
			t.Fatalf("Failed to connect client %d: %v", i, err)
		}
		connections[i] = conn
	}

	// Give server time to register all clients
	time.Sleep(100 * time.Millisecond)

	// Verify all clients registered
	srv.mu.RLock()
	clientCount := len(srv.clients)
	srv.mu.RUnlock()

	if clientCount != numClients {
		t.Errorf("Expected %d clients, got %d", numClients, clientCount)
	}

	// Close all connections
	for i, conn := range connections {
		if conn != nil {
			conn.Close()
		}
		// Stagger closes slightly
		if i < numClients-1 {
			time.Sleep(10 * time.Millisecond)
		}
	}

	// Give server time to unregister all clients
	time.Sleep(100 * time.Millisecond)

	// Verify all clients unregistered
	srv.mu.RLock()
	clientCount = len(srv.clients)
	srv.mu.RUnlock()

	if clientCount != 0 {
		t.Errorf("Expected 0 clients after all disconnects, got %d", clientCount)
	}
}

// Helper function to get map keys
func getKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// Test broadcast message helper
func TestBroadcastMessage(t *testing.T) {
	store, db := createTestStore(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, store, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start hub
	go srv.Run()

	// Create clients
	client1 := &Client{
		server:  srv,
		sendMsg: make(chan interface{}, 256),
		id:      "client1",
	}
	client2 := &Client{
		server:  srv,
		sendMsg: make(chan interface{}, 256),
		id:      "client2",
	}

	srv.register <- client1
	srv.register <- client2
	time.Sleep(20 * time.Millisecond)

	// Send generic message
	testMsg := map[string]interface{}{
		"type":    "test",
		"message": "hello",
	}

	srv.broadcastMessage(testMsg)

	// Verify clients received the message
	select {
	case msg := <-client1.sendMsg:
		if msgMap, ok := msg.(map[string]interface{}); ok {
			if msgMap["message"] != "hello" {
				t.Error("Client1 received incorrect message")
			}
		} else {
			t.Error("Client1 received non-map message")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Client1 did not receive message")
	}

	select {
	case msg := <-client2.sendMsg:
		if msgMap, ok := msg.(map[string]interface{}); ok {
			if msgMap["message"] != "hello" {
				t.Error("Client2 received incorrect message")
			}
		} else {
			t.Error("Client2 received non-map message")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Client2 did not receive message")
	}
}

// TestGetDaemon verifies that GetDaemon returns the Pulse worker pool
func TestGetDaemon(t *testing.T) {
	store, db := createTestStore(t)

	srv, err := NewQNTXServer(db, store, ":memory:", 1)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	// Verify GetDaemon returns the daemon
	daemon := srv.GetDaemon()
	if daemon == nil {
		t.Fatal("GetDaemon() returned nil")
	}

	// Verify we can access the handler registry
	registry := daemon.Registry()
	if registry == nil {
		t.Fatal("daemon.Registry() returned nil")
	}

	// Verify registry has only built-in handlers (e.g. distill if configured)
	handlers := registry.Names()
	for _, h := range handlers {
		if h != "distill" && h != "wal-checkpoint" {
			t.Errorf("Unexpected handler registered: %s", h)
		}
	}
}
