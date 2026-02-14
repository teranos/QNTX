package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/watcher"
	pb "github.com/teranos/QNTX/glyph/proto"
	glyphstorage "github.com/teranos/QNTX/glyph/storage"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/sym"
	"go.uber.org/zap"
)

func setupHandlerWithWatcher(t *testing.T) (*CanvasHandler, *watcher.Engine, *storage.WatcherStore) {
	t.Helper()
	db := qntxtest.CreateTestDB(t)
	canvasStore := glyphstorage.NewCanvasStore(db)
	logger := zap.NewNop().Sugar()
	engine := watcher.NewEngine(db, "http://localhost:877", logger)
	if err := engine.Start(); err != nil {
		t.Fatalf("Failed to start watcher engine: %v", err)
	}
	t.Cleanup(engine.Stop)

	handler := NewCanvasHandler(canvasStore, WithWatcherEngine(engine, logger))
	watcherStore := storage.NewWatcherStore(db)
	return handler, engine, watcherStore
}

func TestCompileSubscriptions_AxToPy(t *testing.T) {
	handler, engine, watcherStore := setupHandlerWithWatcher(t)
	ctx := context.Background()

	// Create AX and Py glyphs with correct symbols
	glyphs := []*glyphstorage.CanvasGlyph{
		{ID: "ax-glyph-1", Symbol: sym.AX, X: 100, Y: 100},
		{ID: "py-glyph-1", Symbol: "py", X: 200, Y: 100},
	}
	for _, g := range glyphs {
		if err := handler.store.UpsertGlyph(ctx, g); err != nil {
			t.Fatalf("UpsertGlyph failed: %v", err)
		}
	}

	// Create the AX glyph's watcher (as ax-glyph.ts would via WebSocket)
	axWatcher := &storage.Watcher{
		ID:                "ax-glyph-ax-glyph-1",
		Name:              "AX Glyph: contact",
		AxQuery:           "contact",
		ActionType:        storage.ActionTypePython,
		MaxFiresPerMinute: 60,
		Enabled:           true,
	}
	if err := watcherStore.Create(ctx, axWatcher); err != nil {
		t.Fatalf("Failed to create AX watcher: %v", err)
	}
	if err := engine.ReloadWatchers(); err != nil {
		t.Fatalf("ReloadWatchers failed: %v", err)
	}

	// Upsert composition with right edge: ax → py
	comp := glyphstorage.CanvasComposition{
		ID: "comp-ax-py",
		Edges: []*pb.CompositionEdge{
			makeEdge("ax-glyph-1", "py-glyph-1", "right", 0),
		},
		X: 100, Y: 100,
	}
	body, _ := json.Marshal(comp)
	req := httptest.NewRequest(http.MethodPost, "/api/canvas/compositions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleCompositions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify meld-edge watcher was created
	meldWatcher, err := watcherStore.Get(ctx, "meld-edge-comp-ax-py-ax-glyph-1-py-glyph-1")
	if err != nil {
		t.Fatalf("Meld-edge watcher not found: %v", err)
	}

	if meldWatcher.ActionType != storage.ActionTypeGlyphExecute {
		t.Errorf("Expected action type glyph_execute, got %s", meldWatcher.ActionType)
	}
	if meldWatcher.AxQuery != "contact" {
		t.Errorf("Expected AxQuery 'contact', got %q", meldWatcher.AxQuery)
	}

	// Verify action data has target glyph info
	var actionData map[string]string
	if err := json.Unmarshal([]byte(meldWatcher.ActionData), &actionData); err != nil {
		t.Fatalf("Failed to parse action data: %v", err)
	}
	if actionData["target_glyph_id"] != "py-glyph-1" {
		t.Errorf("Expected target_glyph_id 'py-glyph-1', got %q", actionData["target_glyph_id"])
	}
	if actionData["target_glyph_type"] != "py" {
		t.Errorf("Expected target_glyph_type 'py', got %q", actionData["target_glyph_type"])
	}
}

func TestCompileSubscriptions_PyToPrompt(t *testing.T) {
	handler, _, watcherStore := setupHandlerWithWatcher(t)
	ctx := context.Background()

	// Create Py and Prompt glyphs
	glyphs := []*glyphstorage.CanvasGlyph{
		{ID: "py-glyph-1", Symbol: "py", X: 100, Y: 100},
		{ID: "prompt-glyph-1", Symbol: sym.SO, X: 200, Y: 100},
	}
	for _, g := range glyphs {
		if err := handler.store.UpsertGlyph(ctx, g); err != nil {
			t.Fatalf("UpsertGlyph failed: %v", err)
		}
	}

	// Upsert composition: py → prompt
	comp := glyphstorage.CanvasComposition{
		ID: "comp-py-prompt",
		Edges: []*pb.CompositionEdge{
			makeEdge("py-glyph-1", "prompt-glyph-1", "right", 0),
		},
		X: 100, Y: 100,
	}
	body, _ := json.Marshal(comp)
	req := httptest.NewRequest(http.MethodPost, "/api/canvas/compositions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleCompositions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify meld-edge watcher uses actor filter (not AX query)
	meldWatcher, err := watcherStore.Get(ctx, "meld-edge-comp-py-prompt-py-glyph-1-prompt-glyph-1")
	if err != nil {
		t.Fatalf("Meld-edge watcher not found: %v", err)
	}

	if meldWatcher.AxQuery != "" {
		t.Errorf("Producer edge should not have AxQuery, got %q", meldWatcher.AxQuery)
	}
	if len(meldWatcher.Filter.Actors) != 1 || meldWatcher.Filter.Actors[0] != "glyph:py-glyph-1" {
		t.Errorf("Expected actors filter [glyph:py-glyph-1], got %v", meldWatcher.Filter.Actors)
	}

	var actionData map[string]string
	if err := json.Unmarshal([]byte(meldWatcher.ActionData), &actionData); err != nil {
		t.Fatalf("Failed to parse action data: %v", err)
	}
	if actionData["target_glyph_type"] != "prompt" {
		t.Errorf("Expected target type 'prompt', got %q", actionData["target_glyph_type"])
	}
}

func TestCompileSubscriptions_DeleteCleansUpWatchers(t *testing.T) {
	handler, _, watcherStore := setupHandlerWithWatcher(t)
	ctx := context.Background()

	// Create glyphs
	glyphs := []*glyphstorage.CanvasGlyph{
		{ID: "py-a", Symbol: "py", X: 100, Y: 100},
		{ID: "py-b", Symbol: "py", X: 200, Y: 100},
	}
	for _, g := range glyphs {
		if err := handler.store.UpsertGlyph(ctx, g); err != nil {
			t.Fatalf("UpsertGlyph failed: %v", err)
		}
	}

	// Create composition: py-a → py-b
	comp := glyphstorage.CanvasComposition{
		ID: "comp-cleanup",
		Edges: []*pb.CompositionEdge{
			makeEdge("py-a", "py-b", "right", 0),
		},
		X: 100, Y: 100,
	}
	body, _ := json.Marshal(comp)
	req := httptest.NewRequest(http.MethodPost, "/api/canvas/compositions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.HandleCompositions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Create failed: %d", w.Code)
	}

	// Verify watcher exists
	_, err := watcherStore.Get(ctx, "meld-edge-comp-cleanup-py-a-py-b")
	if err != nil {
		t.Fatalf("Watcher should exist after create: %v", err)
	}

	// Delete composition
	req = httptest.NewRequest(http.MethodDelete, "/api/canvas/compositions/comp-cleanup", nil)
	w = httptest.NewRecorder()
	handler.HandleCompositions(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("Delete failed: %d", w.Code)
	}

	// Verify watcher was cleaned up
	_, err = watcherStore.Get(ctx, "meld-edge-comp-cleanup-py-a-py-b")
	if err == nil {
		t.Error("Watcher should have been deleted with composition")
	}
}

func TestCompileSubscriptions_BottomEdgesIgnored(t *testing.T) {
	handler, _, watcherStore := setupHandlerWithWatcher(t)
	ctx := context.Background()

	// Create glyphs
	glyphs := []*glyphstorage.CanvasGlyph{
		{ID: "py-1", Symbol: "py", X: 100, Y: 100},
		{ID: "result-1", Symbol: "result", X: 100, Y: 200},
	}
	for _, g := range glyphs {
		if err := handler.store.UpsertGlyph(ctx, g); err != nil {
			t.Fatalf("UpsertGlyph failed: %v", err)
		}
	}

	// Create composition with bottom edge only (result auto-meld)
	comp := glyphstorage.CanvasComposition{
		ID: "comp-bottom",
		Edges: []*pb.CompositionEdge{
			makeEdge("py-1", "result-1", "bottom", 0),
		},
		X: 100, Y: 100,
	}
	body, _ := json.Marshal(comp)
	req := httptest.NewRequest(http.MethodPost, "/api/canvas/compositions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.HandleCompositions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	// No meld-edge watchers should have been created for bottom edges
	watchers, err := watcherStore.List(ctx, false)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	for _, wt := range watchers {
		if wt.ActionType == storage.ActionTypeGlyphExecute {
			t.Errorf("No glyph_execute watchers expected for bottom edges, found: %s", wt.ID)
		}
	}
}

func TestCompileSubscriptions_PyToPy(t *testing.T) {
	handler, _, watcherStore := setupHandlerWithWatcher(t)
	ctx := context.Background()

	// Create two Py glyphs
	glyphs := []*glyphstorage.CanvasGlyph{
		{ID: "py-source", Symbol: "py", X: 100, Y: 100},
		{ID: "py-sink", Symbol: "py", X: 200, Y: 100},
	}
	for _, g := range glyphs {
		if err := handler.store.UpsertGlyph(ctx, g); err != nil {
			t.Fatalf("UpsertGlyph failed: %v", err)
		}
	}

	// Upsert composition: py-source → py-sink
	comp := glyphstorage.CanvasComposition{
		ID: "comp-py-py",
		Edges: []*pb.CompositionEdge{
			makeEdge("py-source", "py-sink", "right", 0),
		},
		X: 100, Y: 100,
	}
	body, _ := json.Marshal(comp)
	req := httptest.NewRequest(http.MethodPost, "/api/canvas/compositions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleCompositions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Verify meld-edge watcher was created with actor filter
	meldWatcher, err := watcherStore.Get(ctx, "meld-edge-comp-py-py-py-source-py-sink")
	if err != nil {
		t.Fatalf("Meld-edge watcher not found: %v", err)
	}

	if meldWatcher.ActionType != storage.ActionTypeGlyphExecute {
		t.Errorf("Expected action type glyph_execute, got %s", meldWatcher.ActionType)
	}

	// Py source uses actor filter (attestations created by the upstream glyph)
	if meldWatcher.AxQuery != "" {
		t.Errorf("Producer edge should not have AxQuery, got %q", meldWatcher.AxQuery)
	}
	if len(meldWatcher.Filter.Actors) != 1 || meldWatcher.Filter.Actors[0] != "glyph:py-source" {
		t.Errorf("Expected actors filter [glyph:py-source], got %v", meldWatcher.Filter.Actors)
	}

	// Verify action data targets py-sink
	var actionData map[string]string
	if err := json.Unmarshal([]byte(meldWatcher.ActionData), &actionData); err != nil {
		t.Fatalf("Failed to parse action data: %v", err)
	}
	if actionData["target_glyph_id"] != "py-sink" {
		t.Errorf("Expected target_glyph_id 'py-sink', got %q", actionData["target_glyph_id"])
	}
	if actionData["target_glyph_type"] != "py" {
		t.Errorf("Expected target_glyph_type 'py', got %q", actionData["target_glyph_type"])
	}
	if actionData["source_glyph_id"] != "py-source" {
		t.Errorf("Expected source_glyph_id 'py-source', got %q", actionData["source_glyph_id"])
	}
}

func TestCompileSubscriptions_StaleEdgeCleanup(t *testing.T) {
	handler, engine, watcherStore := setupHandlerWithWatcher(t)
	ctx := context.Background()

	// Create AX and two Py glyphs
	glyphs := []*glyphstorage.CanvasGlyph{
		{ID: "ax-1", Symbol: sym.AX, X: 100, Y: 100},
		{ID: "py-a", Symbol: "py", X: 200, Y: 100},
		{ID: "py-b", Symbol: "py", X: 300, Y: 100},
	}
	for _, g := range glyphs {
		if err := handler.store.UpsertGlyph(ctx, g); err != nil {
			t.Fatalf("UpsertGlyph failed: %v", err)
		}
	}

	// Create AX watcher (as ax-glyph.ts would)
	axWatcher := &storage.Watcher{
		ID:                "ax-glyph-ax-1",
		Name:              "AX Glyph: contact",
		AxQuery:           "contact",
		ActionType:        storage.ActionTypePython,
		MaxFiresPerMinute: 60,
		Enabled:           true,
	}
	if err := watcherStore.Create(ctx, axWatcher); err != nil {
		t.Fatalf("Failed to create AX watcher: %v", err)
	}
	if err := engine.ReloadWatchers(); err != nil {
		t.Fatalf("ReloadWatchers failed: %v", err)
	}

	// POST composition with 2 right edges: ax→py-a, ax→py-b
	comp := glyphstorage.CanvasComposition{
		ID: "comp-stale",
		Edges: []*pb.CompositionEdge{
			makeEdge("ax-1", "py-a", "right", 0),
			makeEdge("ax-1", "py-b", "right", 1),
		},
		X: 100, Y: 100,
	}
	body, _ := json.Marshal(comp)
	req := httptest.NewRequest(http.MethodPost, "/api/canvas/compositions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.HandleCompositions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("First POST failed: %d: %s", w.Code, w.Body.String())
	}

	// Verify 2 meld-edge watchers exist
	_, err := watcherStore.Get(ctx, "meld-edge-comp-stale-ax-1-py-a")
	if err != nil {
		t.Fatalf("Watcher for py-a should exist: %v", err)
	}
	_, err = watcherStore.Get(ctx, "meld-edge-comp-stale-ax-1-py-b")
	if err != nil {
		t.Fatalf("Watcher for py-b should exist: %v", err)
	}

	// Update composition: remove py-b edge, keep only ax→py-a
	comp.Edges = []*pb.CompositionEdge{
		makeEdge("ax-1", "py-a", "right", 0),
	}
	body, _ = json.Marshal(comp)
	req = httptest.NewRequest(http.MethodPost, "/api/canvas/compositions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	handler.HandleCompositions(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Second POST failed: %d: %s", w.Code, w.Body.String())
	}

	// py-a watcher should still exist
	_, err = watcherStore.Get(ctx, "meld-edge-comp-stale-ax-1-py-a")
	if err != nil {
		t.Fatalf("Watcher for py-a should still exist: %v", err)
	}

	// py-b watcher should be gone (stale edge cleaned up)
	_, err = watcherStore.Get(ctx, "meld-edge-comp-stale-ax-1-py-b")
	if err == nil {
		t.Error("Watcher for py-b should have been cleaned up as stale edge")
	}
}
