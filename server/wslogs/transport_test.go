package wslogs

import (
	"sync"
	"testing"
	"time"
)

// TestTransportBasicRegistration tests basic client registration and unregistration
func TestTransportBasicRegistration(t *testing.T) {
	transport := NewTransport()

	// Initially no clients
	if transport.ClientCount() != 0 {
		t.Errorf("New transport should have 0 clients, got %d", transport.ClientCount())
	}

	// Register a client
	ch := make(chan *Batch, 1)
	transport.RegisterClient("client1", ch)

	if transport.ClientCount() != 1 {
		t.Errorf("Transport should have 1 client, got %d", transport.ClientCount())
	}

	// Register another client
	ch2 := make(chan *Batch, 1)
	transport.RegisterClient("client2", ch2)

	if transport.ClientCount() != 2 {
		t.Errorf("Transport should have 2 clients, got %d", transport.ClientCount())
	}

	// Unregister first client
	transport.UnregisterClient("client1")

	if transport.ClientCount() != 1 {
		t.Errorf("Transport should have 1 client after unregister, got %d", transport.ClientCount())
	}

	// Unregister non-existent client (should not error)
	transport.UnregisterClient("client_does_not_exist")

	if transport.ClientCount() != 1 {
		t.Errorf("Transport should still have 1 client, got %d", transport.ClientCount())
	}
}

// TestTransportSendBatch tests basic batch delivery
func TestTransportSendBatch(t *testing.T) {
	transport := NewTransport()

	ch1 := make(chan *Batch, 1)
	ch2 := make(chan *Batch, 1)

	transport.RegisterClient("client1", ch1)
	transport.RegisterClient("client2", ch2)

	batch := &Batch{
		QueryID: "test_query",
		Messages: []Message{
			{
				Level:   "INFO",
				Logger:  "test",
				Message: "Test message",
			},
		},
		Timestamp: time.Now(),
	}

	transport.SendBatch(batch)

	// Verify both clients received the batch
	select {
	case b := <-ch1:
		if b.QueryID != "test_query" {
			t.Errorf("Client 1 received wrong batch: %s", b.QueryID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Client 1 did not receive batch")
	}

	select {
	case b := <-ch2:
		if b.QueryID != "test_query" {
			t.Errorf("Client 2 received wrong batch: %s", b.QueryID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("Client 2 did not receive batch")
	}
}

// TestTransportSendBatchNil tests that nil batches are not sent
func TestTransportSendBatchNil(t *testing.T) {
	transport := NewTransport()
	ch := make(chan *Batch, 1)
	transport.RegisterClient("client1", ch)

	// Send nil batch
	transport.SendBatch(nil)

	// Channel should be empty
	select {
	case <-ch:
		t.Error("Should not have received nil batch")
	case <-time.After(10 * time.Millisecond):
		// Expected - no message sent
	}
}

// TestTransportSendBatchEmpty tests that empty batches are not sent
func TestTransportSendBatchEmpty(t *testing.T) {
	transport := NewTransport()
	ch := make(chan *Batch, 1)
	transport.RegisterClient("client1", ch)

	// Send empty batch
	batch := &Batch{
		QueryID:  "test",
		Messages: []Message{},
	}
	transport.SendBatch(batch)

	// Channel should be empty
	select {
	case <-ch:
		t.Error("Should not have received empty batch")
	case <-time.After(10 * time.Millisecond):
		// Expected - no message sent
	}
}

// TestTransportNonBlockingSend tests that sends don't block when channel is full
func TestTransportNonBlockingSend(t *testing.T) {
	transport := NewTransport()

	// Create channel with capacity 1
	ch := make(chan *Batch, 1)
	transport.RegisterClient("client1", ch)

	batch1 := &Batch{
		QueryID: "query1",
		Messages: []Message{
			{Message: "Message 1"},
		},
	}
	batch2 := &Batch{
		QueryID: "query2",
		Messages: []Message{
			{Message: "Message 2"},
		},
	}

	// Send first batch - should succeed
	transport.SendBatch(batch1)

	// Send second batch - channel is full, should drop without blocking
	done := make(chan bool)
	go func() {
		transport.SendBatch(batch2)
		done <- true
	}()

	// Should complete immediately without blocking
	select {
	case <-done:
		// Success - didn't block
	case <-time.After(100 * time.Millisecond):
		t.Error("SendBatch blocked when channel was full - should be non-blocking")
	}

	// Verify only first batch was received
	select {
	case b := <-ch:
		if b.QueryID != "query1" {
			t.Errorf("Expected query1, got %s", b.QueryID)
		}
	case <-time.After(10 * time.Millisecond):
		t.Error("Should have received first batch")
	}

	// Channel should now be empty (second batch was dropped)
	select {
	case <-ch:
		t.Error("Should not have received second batch (it should have been dropped)")
	case <-time.After(10 * time.Millisecond):
		// Expected - second batch was dropped
	}
}

// TestTransportConcurrentRegistration tests concurrent client registration
func TestTransportConcurrentRegistration(t *testing.T) {
	transport := NewTransport()

	var wg sync.WaitGroup
	numClients := 50

	// Concurrently register many clients
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ch := make(chan *Batch, 1)
			clientID := string(rune('A' + id))
			transport.RegisterClient(clientID, ch)
		}(i)
	}

	wg.Wait()

	if transport.ClientCount() != numClients {
		t.Errorf("Expected %d clients, got %d", numClients, transport.ClientCount())
	}
}

// TestTransportConcurrentUnregistration tests concurrent client unregistration
func TestTransportConcurrentUnregistration(t *testing.T) {
	transport := NewTransport()

	numClients := 50
	channels := make([]chan *Batch, numClients)

	// Register clients
	for i := 0; i < numClients; i++ {
		channels[i] = make(chan *Batch, 1)
		clientID := string(rune('A' + i))
		transport.RegisterClient(clientID, channels[i])
	}

	// Verify all registered
	if transport.ClientCount() != numClients {
		t.Fatalf("Expected %d clients, got %d", numClients, transport.ClientCount())
	}

	var wg sync.WaitGroup

	// Concurrently unregister all clients
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			clientID := string(rune('A' + id))
			transport.UnregisterClient(clientID)
		}(i)
	}

	wg.Wait()

	if transport.ClientCount() != 0 {
		t.Errorf("Expected 0 clients after unregistration, got %d", transport.ClientCount())
	}
}

// TestTransportConcurrentRegistrationDuringBroadcast tests registering clients while broadcasting
func TestTransportConcurrentRegistrationDuringBroadcast(t *testing.T) {
	transport := NewTransport()

	// Register initial clients
	initialClients := 10
	for i := 0; i < initialClients; i++ {
		ch := make(chan *Batch, 10)
		clientID := string(rune('A' + i))
		transport.RegisterClient(clientID, ch)
	}

	var wg sync.WaitGroup
	stopBroadcast := make(chan bool)

	// Start continuous broadcasting
	wg.Add(1)
	go func() {
		defer wg.Done()
		counter := 0
		for {
			select {
			case <-stopBroadcast:
				return
			default:
				batch := &Batch{
					QueryID: "broadcast",
					Messages: []Message{
						{Message: "Continuous broadcast"},
					},
				}
				transport.SendBatch(batch)
				counter++
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Concurrently register new clients during broadcast
	newClients := 20
	for i := 0; i < newClients; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ch := make(chan *Batch, 10)
			clientID := string(rune('a' + id))
			transport.RegisterClient(clientID, ch)
			time.Sleep(5 * time.Millisecond)
		}(i)
	}

	// Let broadcasts run for a bit
	time.Sleep(50 * time.Millisecond)

	// Stop broadcasting
	close(stopBroadcast)
	wg.Wait()

	// Verify all clients registered successfully
	expectedCount := initialClients + newClients
	if transport.ClientCount() != expectedCount {
		t.Errorf("Expected %d clients, got %d", expectedCount, transport.ClientCount())
	}
}

// TestTransportConcurrentUnregistrationDuringBroadcast tests unregistering clients while broadcasting
func TestTransportConcurrentUnregistrationDuringBroadcast(t *testing.T) {
	transport := NewTransport()

	// Register many clients
	numClients := 30
	clientIDs := make([]string, numClients)
	for i := 0; i < numClients; i++ {
		ch := make(chan *Batch, 10)
		clientIDs[i] = string(rune('A' + i))
		transport.RegisterClient(clientIDs[i], ch)
	}

	var wg sync.WaitGroup
	stopBroadcast := make(chan bool)

	// Start continuous broadcasting
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopBroadcast:
				return
			default:
				batch := &Batch{
					QueryID: "broadcast",
					Messages: []Message{
						{Message: "Continuous broadcast"},
					},
				}
				transport.SendBatch(batch)
				time.Sleep(1 * time.Millisecond)
			}
		}
	}()

	// Concurrently unregister half the clients during broadcast
	removeCount := numClients / 2
	for i := 0; i < removeCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			time.Sleep(time.Duration(id) * time.Millisecond)
			transport.UnregisterClient(clientIDs[id])
		}(i)
	}

	// Let operations run
	time.Sleep(100 * time.Millisecond)

	// Stop broadcasting
	close(stopBroadcast)
	wg.Wait()

	// Verify correct number of clients remain
	expectedRemaining := numClients - removeCount
	if transport.ClientCount() != expectedRemaining {
		t.Errorf("Expected %d clients remaining, got %d", expectedRemaining, transport.ClientCount())
	}
}

// TestTransportConcurrentMixedOperations tests all operations happening concurrently
func TestTransportConcurrentMixedOperations(t *testing.T) {
	transport := NewTransport()

	var wg sync.WaitGroup
	stopOperations := make(chan bool)
	operationDuration := 200 * time.Millisecond

	// Broadcaster goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stopOperations:
				return
			default:
				batch := &Batch{
					QueryID: "mixed_ops",
					Messages: []Message{
						{Message: "Test"},
					},
				}
				transport.SendBatch(batch)
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()

	// Register goroutines
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-stopOperations:
					return
				default:
					ch := make(chan *Batch, 5)
					clientID := string(rune('A'+id)) + time.Now().Format("15:04:05.000")
					transport.RegisterClient(clientID, ch)
					time.Sleep(10 * time.Millisecond)
				}
			}
		}(i)
	}

	// Unregister goroutines (unregister by getting current clients periodically)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopOperations:
					return
				default:
					// Just call UnregisterClient with random IDs
					// Some will exist, some won't - testing robustness
					randomID := string(rune('A' + (time.Now().UnixNano() % 26)))
					transport.UnregisterClient(randomID)
					time.Sleep(15 * time.Millisecond)
				}
			}
		}()
	}

	// Let all operations run concurrently
	time.Sleep(operationDuration)
	close(stopOperations)
	wg.Wait()

	// No assertion on final count - just verify no panics/deadlocks occurred
	finalCount := transport.ClientCount()
	t.Logf("Final client count after mixed concurrent operations: %d", finalCount)
}
