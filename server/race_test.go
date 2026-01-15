package server

import (
	"sync"
	"testing"
	"time"

	"github.com/teranos/QNTX/graph"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/server/wslogs"
)

// TestRace_BroadcastDuringUnregister tests for a race condition where:
// 1. broadcastMessage() copies client list while holding RLock
// 2. Releases RLock and starts iterating/sending
// 3. Meanwhile handleClientUnregister() removes client and closes channels
// 4. broadcastMessage() sends to closed channel -> PANIC
//
// Run with: go test -race -run TestRace_BroadcastDuringUnregister ./server
func TestRace_BroadcastDuringUnregister(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	// Start hub
	go srv.Run()

	// Run multiple iterations to increase chance of hitting the race
	for iteration := 0; iteration < 10; iteration++ {
		// Create many clients
		numClients := 50
		clients := make([]*Client, numClients)
		for i := 0; i < numClients; i++ {
			client := &Client{
				server:  srv,
				send:    make(chan *graph.Graph, 256),
				sendLog: make(chan *wslogs.Batch, 256),
				sendMsg: make(chan interface{}, 256),
				id:      t.Name() + "_client_" + string(rune('A'+i)),
			}
			clients[i] = client
			srv.register <- client
		}

		// Wait for all registrations
		time.Sleep(20 * time.Millisecond)

		// Concurrently: broadcast messages AND unregister clients
		var wg sync.WaitGroup
		stopBroadcast := make(chan struct{})

		// Goroutine 1: Continuously broadcast messages
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopBroadcast:
					return
				default:
					msg := map[string]interface{}{
						"type":    "test",
						"message": "race test",
					}
					srv.broadcastMessage(msg)
					// Small yield to increase interleaving
					time.Sleep(100 * time.Microsecond)
				}
			}
		}()

		// Goroutine 2: Unregister clients one by one
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, client := range clients {
				srv.unregister <- client
				// Stagger unregistrations
				time.Sleep(50 * time.Microsecond)
			}
		}()

		// Let the race condition have time to manifest
		time.Sleep(50 * time.Millisecond)
		close(stopBroadcast)

		wg.Wait()
	}
}

// TestRace_ConcurrentBroadcastAndChannelClose directly tests
// the scenario where a channel is closed while we try to send to it.
// This simulates what happens in broadcastMessage when a client
// is unregistered mid-iteration.
func TestRace_ConcurrentBroadcastAndChannelClose(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	go srv.Run()

	// Run many iterations
	for iteration := 0; iteration < 50; iteration++ {
		// Create a client
		client := &Client{
			server:  srv,
			send:    make(chan *graph.Graph, 1), // Small buffer to increase contention
			sendLog: make(chan *wslogs.Batch, 1),
			sendMsg: make(chan interface{}, 1),
			id:      t.Name() + "_iteration_" + string(rune('A'+(iteration%26))),
		}
		srv.register <- client
		time.Sleep(5 * time.Millisecond)

		var wg sync.WaitGroup

		// Goroutine 1: Rapid broadcasts
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				msg := map[string]interface{}{
					"type": "test",
					"seq":  i,
				}
				srv.broadcastMessage(msg)
			}
		}()

		// Goroutine 2: Unregister immediately
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Small delay to let some broadcasts start
			time.Sleep(time.Microsecond * 10)
			srv.unregister <- client
		}()

		wg.Wait()
		time.Sleep(5 * time.Millisecond)
	}
}

// TestRace_UsageBroadcastDuringClientDisconnect tests the race
// in broadcastUsageUpdate where the lock is released before broadcast.
func TestRace_UsageBroadcastDuringClientDisconnect(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	go srv.Run()

	for iteration := 0; iteration < 20; iteration++ {
		// Register multiple clients
		numClients := 20
		clients := make([]*Client, numClients)
		for i := 0; i < numClients; i++ {
			client := &Client{
				server:  srv,
				send:    make(chan *graph.Graph, 256),
				sendLog: make(chan *wslogs.Batch, 256),
				sendMsg: make(chan interface{}, 256),
				id:      t.Name() + "_client_" + string(rune('A'+i)),
			}
			clients[i] = client
			srv.register <- client
		}
		time.Sleep(10 * time.Millisecond)

		var wg sync.WaitGroup

		// Goroutine 1: Trigger usage broadcasts
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				srv.broadcastUsageUpdate()
				time.Sleep(100 * time.Microsecond)
			}
		}()

		// Goroutine 2: Unregister clients
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, client := range clients {
				srv.unregister <- client
				time.Sleep(200 * time.Microsecond)
			}
		}()

		wg.Wait()
	}
}

// TestRace_GraphBroadcastToDisconnectingClients tests the graph broadcast
// path which uses client.send channel
func TestRace_GraphBroadcastToDisconnectingClients(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	go srv.Run()

	for iteration := 0; iteration < 30; iteration++ {
		// Create clients with small buffers to increase contention
		numClients := 30
		clients := make([]*Client, numClients)
		for i := 0; i < numClients; i++ {
			client := &Client{
				server:  srv,
				send:    make(chan *graph.Graph, 2), // Small buffer
				sendLog: make(chan *wslogs.Batch, 2),
				sendMsg: make(chan interface{}, 2),
				id:      t.Name() + "_c_" + string(rune('A'+i)),
			}
			clients[i] = client
			srv.register <- client
		}
		time.Sleep(10 * time.Millisecond)

		var wg sync.WaitGroup

		// Goroutine 1: Broadcast graphs via the broadcast channel
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				g := &graph.Graph{
					Nodes: []graph.Node{{ID: "node1", Label: "Test"}},
					Links: []graph.Link{},
					Meta:  graph.Meta{},
				}
				srv.broadcast <- g
				time.Sleep(100 * time.Microsecond)
			}
		}()

		// Goroutine 2: Unregister clients rapidly
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, client := range clients {
				srv.unregister <- client
			}
		}()

		wg.Wait()
		time.Sleep(5 * time.Millisecond)
	}
}

// TestRace_MultipleWritersToClientChannels tests the scenario where
// multiple goroutines try to send to client channels while
// the client might be getting unregistered.
func TestRace_MultipleWritersToClientChannels(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	go srv.Run()

	client := &Client{
		server:  srv,
		send:    make(chan *graph.Graph, 10),
		sendLog: make(chan *wslogs.Batch, 10),
		sendMsg: make(chan interface{}, 10),
		id:      "multi_writer_test",
	}
	srv.register <- client
	time.Sleep(10 * time.Millisecond)

	var wg sync.WaitGroup

	// Multiple broadcast goroutines
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				srv.broadcastMessage(map[string]interface{}{
					"type":     "test",
					"writer":   id,
					"sequence": j,
				})
			}
		}(i)
	}

	// Unregister after a short delay
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond)
		srv.unregister <- client
	}()

	wg.Wait()
}

// TestRace_PluginMuxInitialization tests that concurrent requests to the same plugin
// during first initialization use sync.Once correctly and don't race.
// This test verifies fix for Issue #2 from PR review (race condition in lazy mux init).
//
// Run with: go test -race -run TestRace_PluginMuxInitialization ./server
func TestRace_PluginMuxInitialization(t *testing.T) {
	t.Skip("Requires plugin infrastructure setup - test documents expected behavior")

	// This test would verify:
	// 1. Multiple concurrent HTTP requests hit /api/plugin/route
	// 2. All requests trigger lazy mux initialization simultaneously
	// 3. sync.Once ensures only ONE goroutine initializes
	// 4. Other goroutines block until initialization completes
	// 5. No sleep-based polling, no race conditions
	// 6. All requests eventually succeed with same mux

	// Test structure (when implemented):
	// - Create QNTXServer with mock plugin
	// - Launch 50 concurrent requests to /api/mockplugin/health
	// - Verify plugin.Initialize() called exactly once
	// - Verify all requests get same mux instance
	// - Verify no 503 errors (would indicate sleep-retry logic)
}
