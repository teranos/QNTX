package server

import (
	"testing"
	"time"

	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/server/syscap"
)

// TestSendSystemCapabilities verifies that system capabilities are sent to clients
func TestSendSystemCapabilities(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)

	srv, err := NewQNTXServer(db, store, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start server
	go srv.Run()

	// Create a mock client
	client := &Client{
		server:  srv,
		sendMsg: make(chan interface{}, 256),
		id:      "test_client_capabilities",
	}

	// Register client first
	srv.register <- client
	time.Sleep(10 * time.Millisecond)

	// Call sendSystemCapabilitiesToClient
	srv.sendSystemCapabilitiesToClient(client)

	// Check if message was sent
	select {
	case msg := <-client.sendMsg:
		// Verify it's a syscap.Message
		capMsg, ok := msg.(syscap.Message)
		if !ok {
			t.Fatalf("Expected syscap.Message, got %T", msg)
		}

		// Verify message fields
		if capMsg.Type != "system_capabilities" {
			t.Errorf("Message type = %q, want %q", capMsg.Type, "system_capabilities")
		}

		t.Logf("System capabilities: storage=%s, parser=%s", capMsg.StorageBackend, capMsg.ParserBackend)

	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for system capabilities message")
	}
}

// TestSendSystemCapabilities_ClosedClient verifies graceful handling when client disconnects
func TestSendSystemCapabilities_ClosedClient(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)

	srv, err := NewQNTXServer(db, store, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start server
	go srv.Run()

	// Create a client with closed channel
	client := &Client{
		server:  srv,
		sendMsg: make(chan interface{}),
		id:      "test_client_closed",
	}

	// Close the sendMsg channel immediately to simulate disconnected client
	close(client.sendMsg)

	// This should not panic - should use default case
	srv.sendSystemCapabilitiesToClient(client)

	// If we get here without panic, test passes
	t.Log("Successfully handled closed client channel")
}

// TestSendSystemCapabilities_FullChannel verifies handling of full channel
func TestSendSystemCapabilities_FullChannel(t *testing.T) {
	store, db := qntxtest.CreateTestStore(t)

	srv, err := NewQNTXServer(db, store, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start server
	go srv.Run()

	// Create a client with unbuffered channel (will fill immediately)
	client := &Client{
		server:  srv,
		sendMsg: make(chan interface{}), // unbuffered
		id:      "test_client_full",
	}

	// Don't read from the channel, so it's always full
	// This should not block - should use default case
	done := make(chan bool)
	go func() {
		srv.sendSystemCapabilitiesToClient(client)
		done <- true
	}()

	select {
	case <-done:
		t.Log("Successfully handled full client channel without blocking")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("sendSystemCapabilitiesToClient blocked on full channel")
	}
}
