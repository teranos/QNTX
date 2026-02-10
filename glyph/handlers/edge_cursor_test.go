package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/watcher"
	glyphstorage "github.com/teranos/QNTX/glyph/storage"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"go.uber.org/zap"
)

// TestEdgeCursor_AppliedOnReload verifies the critical path:
// 1. Meld edge watcher is created with cursor in DB
// 2. Engine reload applies cursor as TimeStart filter
// 3. Attestations before cursor are skipped by matchesFilter
func TestEdgeCursor_AppliedOnReload(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zap.NewNop().Sugar()
	engine := watcher.NewEngine(db, "http://localhost:877", logger)
	if err := engine.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	t.Cleanup(engine.Stop)

	watcherStore := storage.NewWatcherStore(db)
	ctx := context.Background()

	// Create a meld-edge watcher with glyph_execute action
	actionData, _ := json.Marshal(map[string]string{
		"target_glyph_id":   "py-target",
		"target_glyph_type": "py",
		"composition_id":    "comp-1",
		"source_glyph_id":   "ax-source",
	})

	w := &storage.Watcher{
		ID:                "meld-edge-comp-1-ax-source-py-target",
		Name:              "test edge",
		Enabled:           true,
		ActionType:        storage.ActionTypeGlyphExecute,
		ActionData:        string(actionData),
		MaxFiresPerMinute: 60,
	}
	w.Filter.Actors = []string{"glyph:ax-source"}

	if err := watcherStore.Create(ctx, w); err != nil {
		t.Fatalf("Failed to create watcher: %v", err)
	}

	// Insert a cursor — pretend we already processed an attestation at this time
	cursorTime := time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC)
	_, err := db.ExecContext(ctx, `
		INSERT INTO composition_edge_cursors (composition_id, from_glyph_id, to_glyph_id, last_processed_id, last_processed_at)
		VALUES (?, ?, ?, ?, ?)`,
		"comp-1", "ax-source", "py-target", "AS-already-processed", cursorTime)
	if err != nil {
		t.Fatalf("Failed to insert cursor: %v", err)
	}

	// Reload watchers — should apply cursor as TimeStart
	if err := engine.ReloadWatchers(); err != nil {
		t.Fatalf("ReloadWatchers failed: %v", err)
	}

	// Get the watcher back and verify TimeStart was set
	loaded, ok := engine.GetWatcher(w.ID)
	if !ok {
		t.Fatalf("Watcher %s not found after reload", w.ID)
	}

	if loaded.Filter.TimeStart == nil {
		t.Fatal("Expected TimeStart to be set from cursor, got nil")
	}

	if !loaded.Filter.TimeStart.Equal(cursorTime) {
		t.Errorf("TimeStart = %v, want %v", loaded.Filter.TimeStart, cursorTime)
	}

	// Verify an attestation BEFORE the cursor is rejected
	oldAttestation := &types.As{
		ID:        "AS-old",
		Subjects:  []string{"test"},
		Actors:    []string{"glyph:ax-source"},
		Timestamp: cursorTime.Add(-1 * time.Hour),
	}

	// OnAttestationCreated should NOT fire executeGlyph for old attestation
	// (it would fail anyway since no HTTP server, but the filter should reject it)
	// We verify by checking that no error is recorded for the watcher
	engine.OnAttestationCreated(oldAttestation)
	time.Sleep(50 * time.Millisecond) // let async dispatch settle

	// The old attestation should have been filtered out — no fire recorded
	var fireCount int
	db.QueryRowContext(ctx, "SELECT fire_count FROM watchers WHERE id = ?", w.ID).Scan(&fireCount)
	if fireCount != 0 {
		t.Errorf("Expected 0 fires (old attestation should be filtered), got %d", fireCount)
	}
}

// TestEdgeCursor_DeletedWithComposition verifies cascade delete of cursors
func TestEdgeCursor_DeletedWithComposition(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	canvasStore := glyphstorage.NewCanvasStore(db)
	logger := zap.NewNop().Sugar()
	engine := watcher.NewEngine(db, "http://localhost:877", logger)
	if err := engine.Start(); err != nil {
		t.Fatalf("Failed to start engine: %v", err)
	}
	t.Cleanup(engine.Stop)

	handler := NewCanvasHandler(canvasStore, WithWatcherEngine(engine, logger))
	ctx := context.Background()

	// Create a composition
	comp := &glyphstorage.CanvasComposition{
		ID: "comp-to-delete",
	}
	if err := canvasStore.UpsertComposition(ctx, comp); err != nil {
		t.Fatalf("Failed to create composition: %v", err)
	}

	// Insert cursors for this composition
	_, err := db.ExecContext(ctx, `
		INSERT INTO composition_edge_cursors (composition_id, from_glyph_id, to_glyph_id, last_processed_id, last_processed_at)
		VALUES (?, ?, ?, ?, ?)`,
		"comp-to-delete", "from-a", "to-b", "AS-1", time.Now())
	if err != nil {
		t.Fatalf("Failed to insert cursor: %v", err)
	}

	// Delete via HTTP handler
	req, _ := http.NewRequestWithContext(ctx, http.MethodDelete,
		"/api/canvas/compositions/comp-to-delete", nil)
	rr := httptest.NewRecorder()
	req.SetPathValue("id", "comp-to-delete")
	handler.HandleCompositions(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("Expected 204, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify cursor was deleted
	var count int
	db.QueryRowContext(ctx, "SELECT COUNT(*) FROM composition_edge_cursors WHERE composition_id = ?",
		"comp-to-delete").Scan(&count)
	if count != 0 {
		t.Errorf("Expected 0 cursors after composition delete, got %d", count)
	}
}
