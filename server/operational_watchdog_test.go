package server

import (
	"testing"
	"time"
)

// The store closed mid-run and every subsystem caught the error and carried on:
// the pulse ticker failed 987 times, login returned 500 in 0 ms, /health said ok.
// Nothing decided. This is the thing that decides.
func TestAnUnreadableOperationalStoreStopsTheProcess(t *testing.T) {
	store, db := createTestStore(t)

	srv, err := NewQNTXServer(db, store, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	stopped := make(chan error, 1)
	go srv.WatchOperationalStore(func(reason error) { stopped <- reason })

	db.Close()

	select {
	case reason := <-stopped:
		if reason == nil {
			t.Fatal("stopped with no reason")
		}
	case <-time.After(3 * operationalCheckInterval):
		t.Fatal("the store was unreadable and the process was not stopped")
	}
}

// A readable store is the ordinary case, and a watchdog that fires on it would
// restart the node every few seconds forever.
func TestAReadableOperationalStoreStopsNothing(t *testing.T) {
	store, db := createTestStore(t)
	defer db.Close()

	srv, err := NewQNTXServer(db, store, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create QNTXServer: %v", err)
	}

	stopped := make(chan error, 1)
	go srv.WatchOperationalStore(func(reason error) { stopped <- reason })

	select {
	case reason := <-stopped:
		t.Fatalf("stopped a healthy node: %v", reason)
	case <-time.After(2 * operationalCheckInterval):
	}
}
