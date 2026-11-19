//go:build cgo && rustetcd

package etcdcgo

import (
	"sync"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

// 「[Opus 5.5] held is a store that holds every put until released, as an etcd
// that does not answer holds each native call. started says each put began.」
type held struct {
	release chan struct{}
	freed   sync.Once
	started chan string
	mu      sync.Mutex
	put     []string
}

func holding() *held {
	return &held{release: make(chan struct{}), started: make(chan string, 64)}
}

func (h *held) Put(as *types.As) error {
	h.started <- as.ID
	<-h.release
	h.mu.Lock()
	defer h.mu.Unlock()
	h.put = append(h.put, as.ID)
	return nil
}

func (h *held) Close() {}

// 「[Opus 5.5] free lets every held put go, once, however often it is called.」
func (h *held) free() { h.freed.Do(func() { close(h.release) }) }

func (h *held) puts() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.put)
}

// 「[Opus 5.5] failures counts what a window handed to failed.」
type failures struct {
	mu  sync.Mutex
	got []error
}

func (f *failures) add(err error) { f.mu.Lock(); f.got = append(f.got, err); f.mu.Unlock() }
func (f *failures) n() int        { f.mu.Lock(); defer f.mu.Unlock(); return len(f.got) }

// 「[Opus 5.5] The store is freed before the window is closed, whatever ended the test:
// a window closed first waits on a put that is never let go (CodeRabbit on #1765).」
func cleanup(t *testing.T, store *held, w *Window) {
	t.Cleanup(func() { store.free(); w.Close() })
}

func handed(t *testing.T, w *Window, id string) {
	t.Helper()
	done := make(chan struct{})
	go func() { w.OnAttestationCreated(attestation(id)); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("handing %s to the window waited on etcd", id)
	}
}

// 「[Opus 5.5] While etcd does not answer, what waits for it is bounded: past the
// queue, an attestation is handed to failed at once and nothing waits for it
// (CodeRabbit on #1765). What fit is put once etcd answers.」
func TestTheWindowWaitsForNoMoreThanItsQueue(t *testing.T) {
	store := holding()
	var failed failures
	w := NewWindowOver(store, 2, time.Second, failed.add)
	cleanup(t, store, w)

	// 「[Opus 5.5] The worker takes AS-1 and is held at its put before the rest come:
	// two then fit the queue and one is over (CodeRabbit on #1765: a sleep proved nothing).」
	handed(t, w, "AS-1")
	if id := <-store.started; id != "AS-1" {
		t.Fatalf("the worker began with %s", id)
	}
	for _, id := range []string{"AS-2", "AS-3", "AS-4"} {
		handed(t, w, id)
	}
	if n := failed.n(); n != 1 {
		t.Fatalf("want the one past the queue handed to failed, got %d: %v", n, failed.got)
	}

	store.free()
	deadline := time.Now().Add(2 * time.Second)
	for store.puts() < 3 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := store.puts(); got != 3 {
		t.Fatalf("want the three that fit put once etcd answers, got %d", got)
	}
}

// 「[Opus 5.5] Handing to a window and closing it at once is no race: the send and the
// close are one lock's (CodeRabbit on #1765). Run with -race.」
func TestHandingAndClosingAtOnceIsNoRace(t *testing.T) {
	store := holding()
	store.free()
	var failed failures
	w := NewWindowOver(store, 8, time.Second, failed.add)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				w.OnAttestationCreated(attestation("AS-race"))
			}
		}()
	}
	w.Close()
	wg.Wait()
}

// 「[Opus 5.5] Closing waits no longer than its drain for what is queued: what is not
// put by then is handed to failed, the put under way is let finish, and the store
// is closed after it (CodeRabbit on #1765: a stalled etcd held a namespace's restart
// for hours).」
func TestClosingWaitsNoLongerThanItsDrain(t *testing.T) {
	store := holding()
	var failed failures
	w := NewWindowOver(store, 4, 50*time.Millisecond, failed.add)
	cleanup(t, store, w)

	handed(t, w, "AS-1")
	<-store.started
	handed(t, w, "AS-2")
	handed(t, w, "AS-3")

	closed := make(chan struct{})
	go func() { w.Close(); close(closed) }()
	time.Sleep(200 * time.Millisecond)
	select {
	case <-closed:
		t.Fatal("Close returned while a put was still under way")
	default:
	}
	if n := failed.n(); n != 2 {
		t.Fatalf("want the two still queued at the drain handed to failed, got %d: %v", n, failed.got)
	}
	store.free()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return once the put under way finished")
	}
	if got := store.puts(); got != 1 {
		t.Fatalf("want only the put under way made, got %d", got)
	}
}
