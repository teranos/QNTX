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
	"github.com/teranos/QNTX/graph"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/server/wslogs"
)

// createTestDB is a local alias for qntxtest.CreateTestDB
func createTestDB(t *testing.T) *sql.DB {
	return qntxtest.CreateTestDB(t)
}

// Test basic server creation and initialization
func TestNewQNTXServer(t *testing.T) {
	db := createTestDB(t)

	srv, err := NewQNTXServer(db, ":memory:", 1)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	if srv.db != db {
		t.Error("Server database not set correctly")
	}

	if int(srv.verbosity.Load()) != 1 {
		t.Errorf("Server verbosity = %d, want 1", int(srv.verbosity.Load()))
	}

	if srv.builder == nil {
		t.Error("Server builder not initialized")
	}

	if srv.clients == nil {
		t.Error("Server clients map not initialized")
	}

	if srv.logTransport == nil {
		t.Error("Server log transport not initialized")
	}

	if srv.wsCore == nil {
		t.Error("Server WebSocket core not initialized")
	}
}

// Test that the hub goroutine handles client registration
func TestServerHubRegistration(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start hub in background
	go srv.Run()

	// Create a mock client
	client := &Client{
		server:  srv,
		send:    make(chan *graph.Graph, 256),
		sendLog: make(chan *wslogs.Batch, 256),
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

	// Verify client was registered in log transport
	if srv.logTransport.ClientCount() != 1 {
		t.Errorf("Log transport should have 1 client, got %d", srv.logTransport.ClientCount())
	}
}

// Test that the hub goroutine handles client unregistration
func TestServerHubUnregistration(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start hub in background
	go srv.Run()

	// Create and register a client
	client := &Client{
		server:  srv,
		send:    make(chan *graph.Graph, 256),
		sendLog: make(chan *wslogs.Batch, 256),
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

	// Verify client was unregistered from log transport
	if srv.logTransport.ClientCount() != 0 {
		t.Errorf("Log transport should have 0 clients, got %d", srv.logTransport.ClientCount())
	}

	// Verify channels were closed (reading from closed channel returns zero value immediately)
	select {
	case _, ok := <-client.send:
		if ok {
			t.Error("Client send channel should be closed")
		}
	case <-time.After(10 * time.Millisecond):
		t.Error("Client send channel was not closed")
	}
}

// Test concurrent client registration
func TestServerConcurrentRegistration(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, ":memory:", 0)
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
				send:    make(chan *graph.Graph, 256),
				sendLog: make(chan *wslogs.Batch, 256),
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

// Test port fallback logic
func TestFindAvailablePort(t *testing.T) {
	// Test finding from a high port that should be available
	port, err := findAvailablePort(50000)
	if err != nil {
		t.Fatalf("Failed to find available port: %v", err)
	}

	if port < 50000 || port > 50010 {
		t.Errorf("Port %d is outside expected range 50000-50010", port)
	}
}

// Test WebSocket upgrade handler
func TestHandleWebSocket(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, ":memory:", 0)
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
	conn, _, err := dialer.Dial(wsURL, nil)
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
	db := createTestDB(t)
	defer db.Close()

	// Insert test data using production schema
	_, err := db.Exec(`
		INSERT INTO attestations (id, subjects, predicates, contexts, actors, timestamp, source, attributes, created_at)
		VALUES ('ASTEST001', '["alice"]', '["is"]', '["TEST"]', '["system"]', datetime('now'), 'test', '{}', datetime('now'))
	`)
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	srv, err := NewQNTXServer(db, ":memory:", 0)
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
	conn, _, err := dialer.Dial(wsURL, nil)
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
	db := createTestDB(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, ":memory:", 0)
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
	conn, _, err := dialer.Dial(wsURL, nil)
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
	db := createTestDB(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, ":memory:", 0)
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
		conn, _, err := dialer.Dial(wsURL, nil)
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

// Test broadcast to multiple clients
func TestBroadcastToMultipleClients(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start hub
	go srv.Run()

	// Create and register multiple clients
	numClients := 3
	clients := make([]*Client, numClients)
	for i := 0; i < numClients; i++ {
		client := &Client{
			server:  srv,
			send:    make(chan *graph.Graph, 256),
			sendLog: make(chan *wslogs.Batch, 256),
			sendMsg: make(chan interface{}, 256),
			id:      fmt.Sprintf("test_client_%d", i),
		}
		clients[i] = client
		srv.register <- client
	}

	// Wait for registration
	time.Sleep(50 * time.Millisecond)

	// Create test graph
	testGraph := &graph.Graph{
		Nodes: []graph.Node{{ID: "test1", Label: "Test Node"}},
		Links: []graph.Link{},
		Meta:  graph.Meta{},
	}

	// Broadcast graph
	srv.broadcast <- testGraph

	// Verify all clients received the graph
	time.Sleep(50 * time.Millisecond)
	for i, client := range clients {
		select {
		case receivedGraph := <-client.send:
			if len(receivedGraph.Nodes) != 1 || receivedGraph.Nodes[0].ID != "test1" {
				t.Errorf("Client %d received incorrect graph", i)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("Client %d did not receive broadcast", i)
		}
	}
}

// Test slow client removal during broadcast
func TestSlowClientRemoval(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start hub
	go srv.Run()

	// Create a slow client with tiny buffer
	slowClient := &Client{
		server:  srv,
		send:    make(chan *graph.Graph, 1), // Small buffer
		sendLog: make(chan *wslogs.Batch, 1),
		sendMsg: make(chan interface{}, 1),
		id:      "slow_client",
	}
	srv.register <- slowClient
	time.Sleep(10 * time.Millisecond)

	// Create a normal client
	fastClient := &Client{
		server:  srv,
		send:    make(chan *graph.Graph, 256),
		sendLog: make(chan *wslogs.Batch, 256),
		sendMsg: make(chan interface{}, 256),
		id:      "fast_client",
	}
	srv.register <- fastClient
	time.Sleep(10 * time.Millisecond)

	// Verify both clients registered
	srv.mu.RLock()
	clientCount := len(srv.clients)
	srv.mu.RUnlock()
	if clientCount != 2 {
		t.Fatalf("Expected 2 clients, got %d", clientCount)
	}

	// Send multiple graphs to overflow slow client's buffer
	for i := 0; i < 10; i++ {
		testGraph := &graph.Graph{
			Nodes: []graph.Node{{ID: fmt.Sprintf("node%d", i), Label: "Test"}},
			Links: []graph.Link{},
			Meta:  graph.Meta{},
		}
		srv.broadcast <- testGraph
		time.Sleep(5 * time.Millisecond)
	}

	// Give time for slow client removal
	time.Sleep(100 * time.Millisecond)

	// Verify slow client was removed but fast client remains
	srv.mu.RLock()
	clientCount = len(srv.clients)
	_, slowExists := srv.clients[slowClient]
	_, fastExists := srv.clients[fastClient]
	srv.mu.RUnlock()

	if slowExists {
		t.Error("Slow client should have been removed")
	}
	if !fastExists {
		t.Error("Fast client should still be connected")
	}
	if clientCount != 1 {
		t.Errorf("Expected 1 client after slow client removal, got %d", clientCount)
	}

	// Verify broadcastDrops counter was incremented
	drops := srv.broadcastDrops.Load()
	if drops == 0 {
		t.Error("Broadcast drops counter should be > 0")
	}
}

// Test broadcast message helper
func TestBroadcastMessage(t *testing.T) {
	db := createTestDB(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start hub
	go srv.Run()

	// Create clients
	client1 := &Client{
		server:  srv,
		send:    make(chan *graph.Graph, 256),
		sendLog: make(chan *wslogs.Batch, 256),
		sendMsg: make(chan interface{}, 256),
		id:      "client1",
	}
	client2 := &Client{
		server:  srv,
		send:    make(chan *graph.Graph, 256),
		sendLog: make(chan *wslogs.Batch, 256),
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

	sent := srv.broadcastMessage(testMsg)

	// Verify message was sent to both clients
	if sent != 2 {
		t.Errorf("Expected message sent to 2 clients, got %d", sent)
	}

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
