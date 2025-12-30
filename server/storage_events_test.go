package server

import (
	"testing"

	qntxtest "github.com/teranos/QNTX/internal/testing"
	"go.uber.org/zap"
)

// TestStorageEventsPoller_InitializesWithMaxID verifies that the poller
// starts with MAX(id) to avoid broadcasting historical events on server startup
func TestStorageEventsPoller_InitializesWithMaxID(t *testing.T) {
	db := qntxtest.CreateTestDB(t)

	// Insert historical storage events (simulating events from previous server run)
	_, err := db.Exec(`
		INSERT INTO storage_events (event_type, actor, context, deletions_count, timestamp)
		VALUES
			('storage_warning', 'test@user', 'PROJECT', 13, datetime('now')),
			('actor_context_limit', 'test@user', 'PROJECT', 3, datetime('now')),
			('storage_warning', 'alice@test', 'PROJECT_A', 14, datetime('now'))
	`)
	if err != nil {
		t.Fatalf("Failed to insert historical events: %v", err)
	}

	// Get the max ID before creating poller
	var maxID int64
	err = db.QueryRow("SELECT MAX(id) FROM storage_events").Scan(&maxID)
	if err != nil {
		t.Fatalf("Failed to get max ID: %v", err)
	}

	if maxID != 3 {
		t.Fatalf("Expected 3 historical events with maxID=3, got maxID=%d", maxID)
	}

	// Create server (not needed for poller creation, but follows production pattern)
	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	logger := zap.NewNop().Sugar()
	poller := NewStorageEventsPoller(db, srv, logger)

	// Verify lastID initialized to max (avoiding re-broadcast of historical events)
	if poller.lastID != maxID {
		t.Errorf("Expected lastID=%d (max existing ID), got %d", maxID, poller.lastID)
	}

	if poller.lastID != 3 {
		t.Errorf("Expected poller to skip 3 historical events, lastID should be 3, got %d", poller.lastID)
	}
}
