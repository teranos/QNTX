package wslogs

import (
	"sync"
)

// SendFunc is called to send log batches to a client via the broadcast worker
type SendFunc func(clientID string, batch *Batch)

// Transport handles sending log batches to WebSocket clients
// Uses a callback to route sends through the broadcast worker (thread-safe)
type Transport struct {
	clients  map[string]chan *Batch // Track registered clients (for registration only)
	sendFunc SendFunc               // Callback to broadcast worker
	mu       sync.RWMutex
}

// NewTransport creates a new WebSocket log transport
// Call SetSendFunc() after server initialization to enable broadcast routing
func NewTransport() *Transport {
	return &Transport{
		clients: make(map[string]chan *Batch),
	}
}

// SetSendFunc sets the callback for sending batches (must be called after server init)
func (t *Transport) SetSendFunc(fn SendFunc) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sendFunc = fn
}

// RegisterClient registers a client to receive log batches
// The channel is stored for reference but sends go through sendFunc
func (t *Transport) RegisterClient(id string, ch chan *Batch) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.clients[id] = ch
}

// UnregisterClient removes a client from receiving log batches
func (t *Transport) UnregisterClient(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.clients, id)
}

// SendBatch sends a log batch to all registered clients
// In production: Routes through broadcast worker via sendFunc (thread-safe)
// In tests: Falls back to direct channel sends if sendFunc is nil
func (t *Transport) SendBatch(batch *Batch) {
	if batch == nil || len(batch.Messages) == 0 {
		return
	}

	t.mu.RLock()
	sendFunc := t.sendFunc

	if sendFunc == nil {
		// Fallback mode for tests: send directly to registered channels
		// Production code should always set sendFunc via SetSendFunc()
		// Note: This path indicates SetSendFunc was not called during initialization
		for _, ch := range t.clients {
			select {
			case ch <- batch:
				// Sent successfully
			default:
				// Channel full - skip (non-blocking)
			}
		}
		t.mu.RUnlock()
		return
	}

	// Production mode: route through broadcast worker
	clientIDs := make([]string, 0, len(t.clients))
	for clientID := range t.clients {
		clientIDs = append(clientIDs, clientID)
	}
	t.mu.RUnlock()

	// Send to each client via broadcast worker (thread-safe)
	for _, clientID := range clientIDs {
		sendFunc(clientID, batch)
	}
}

// ClientCount returns the number of registered clients
func (t *Transport) ClientCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.clients)
}
