package wslogs

import (
	"sync"
)

// Transport handles sending log batches to WebSocket clients
type Transport struct {
	clients map[string]chan *Batch
	mu      sync.RWMutex
}

// NewTransport creates a new WebSocket log transport
func NewTransport() *Transport {
	return &Transport{
		clients: make(map[string]chan *Batch),
	}
}

// RegisterClient registers a client to receive log batches
// The channel should have adequate buffering to prevent blocking
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
// If a client's channel is full, the batch is dropped for that client
func (t *Transport) SendBatch(batch *Batch) {
	if batch == nil || len(batch.Messages) == 0 {
		return
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	for clientID, ch := range t.clients {
		select {
		case ch <- batch:
			// Successfully sent
		default:
			// Channel full - drop batch
			// In production, could increment a metric here
			_ = clientID // Avoid unused variable
		}
	}
}

// ClientCount returns the number of registered clients
func (t *Transport) ClientCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.clients)
}
