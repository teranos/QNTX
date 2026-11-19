//go:build cgo && rustetcd

package etcdcgo

import (
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
)

// 「[Opus 5.5] endpoint is the etcd QNTX_ETCD_ENDPOINT names, which
// 「[Opus 5.5] scripts/with-etcd.sh starts. Unset is a failure: a test that passes without」
// 「[Opus 5.5] an etcd says nothing.」」
func endpoint(t *testing.T) string {
	t.Helper()
	e := os.Getenv("QNTX_ETCD_ENDPOINT")
	if e == "" {
		t.Fatal("QNTX_ETCD_ENDPOINT names no etcd to test against")
	}
	return e
}

// 「[Opus 5.5] open is a store in a namespace no other test touches.」
func open(t *testing.T) *Store {
	t.Helper()
	ns := fmt.Sprintf("go%d_%d", os.Getpid(), time.Now().UnixNano())
	store, err := Open(endpoint(t), ns)
	if err != nil {
		t.Fatalf("Open in %s: %v", ns, err)
	}
	t.Cleanup(store.Close)
	return store
}

func attestation(id string) *types.As {
	at := time.UnixMilli(1_700_000_000_000)
	return &types.As{
		ID:         id,
		Subjects:   []string{"mycelia"},
		Predicates: []string{"reads"},
		Contexts:   []string{"etcd"},
		Actors:     []string{"human:bob"},
		Timestamp:  at,
		Source:     "test",
		CreatedAt:  at,
	}
}

// 「[Opus 5.5] The window takes an attestation once: the second put of the same
// 「[Opus 5.5] id is refused as already held, which says the first one landed.」」
func TestTheWindowHoldsWhatItWasHanded(t *testing.T) {
	ns := fmt.Sprintf("go%d_%d", os.Getpid(), time.Now().UnixNano())
	store, err := Open(endpoint(t), ns)
	if err != nil {
		t.Fatalf("Open in %s: %v", ns, err)
	}
	var failed []error
	w := NewWindow(store, func(err error) { failed = append(failed, err) })

	w.OnAttestationCreated(attestation("AS-window"))
	// 「[Opus 5.5] One worker puts: Close waits for what it holds, and closes the store.」
	w.Close()
	if len(failed) > 0 {
		t.Fatalf("the window refused a new attestation: %v", failed)
	}
	again, err := Open(endpoint(t), ns)
	if err != nil {
		t.Fatalf("Open in %s again: %v", ns, err)
	}
	t.Cleanup(again.Close)
	err = again.Put(attestation("AS-window"))
	if !ats.AlreadyExists(err) {
		t.Fatalf("a second put of AS-window: want already-exists, got %v", err)
	}
}

// 「[Opus 5.5] A write the window does not take is handed to failed, once.」
func TestAWriteTheWindowDoesNotTakeIsSaid(t *testing.T) {
	store := open(t)
	var failed []error
	w := NewWindow(store, func(err error) { failed = append(failed, err) })

	empty := attestation("AS-empty")
	empty.Subjects = nil
	w.OnAttestationCreated(empty)
	w.Close()
	if len(failed) != 1 {
		t.Fatalf("want the refused write said once, said %d times", len(failed))
	}
	if !ats.Unwritable(failed[0]) {
		t.Fatalf("want an unwritable refusal, got %v", failed[0])
	}
}

func TestAnEndpointNobodyAnswersIsAnError(t *testing.T) {
	if _, err := Open("http://127.0.0.1:1", "nobody"); err == nil {
		t.Fatal("Open against a closed port returned no error")
	}
}
