//go:build cgo && rustetcd

package etcdcgo

import (
	"sync"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

// 「[Opus 5.5] held is a store that holds every put until released, as an etcd
// that does not answer holds each native call.」
type held struct {
	release chan struct{}
	mu      sync.Mutex
	put     []string
}

func (h *held) Put(as *types.As) error {
	<-h.release
	h.mu.Lock()
	defer h.mu.Unlock()
	h.put = append(h.put, as.ID)
	return nil
}

func (h *held) Close() {}

func (h *held) puts() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.put)
}

// 「[Opus 5.5] While etcd does not answer, what waits for it is bounded: past the
// queue, an attestation is handed to failed at once and nothing waits for it
// (CodeRabbit on #1765). What fit is put once etcd answers.」
func TestTheWindowWaitsForNoMoreThanItsQueue(t *testing.T) {
	store := &held{release: make(chan struct{})}
	var mu sync.Mutex
	var failed []error
	w := NewWindowOver(store, 2, func(err error) { mu.Lock(); failed = append(failed, err); mu.Unlock() })
	defer w.Close()

	// 「[Opus 5.5] One taken by the worker and held at the put, two in the queue, one over.」
	for _, id := range []string{"AS-1", "AS-2", "AS-3", "AS-4"} {
		done := make(chan struct{})
		go func() { w.OnAttestationCreated(attestation(id)); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("handing %s to the window waited on etcd", id)
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	over := len(failed)
	mu.Unlock()
	if over != 1 {
		t.Fatalf("want the one past the queue handed to failed, got %d: %v", over, failed)
	}

	close(store.release)
	deadline := time.Now().Add(2 * time.Second)
	for store.puts() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := store.puts(); got != 3 {
		t.Fatalf("want the three that fit put once etcd answers, got %d", got)
	}
}
