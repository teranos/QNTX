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
	pb "github.com/teranos/QNTX/element/proto"
	elementstorage "github.com/teranos/QNTX/element/storage"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/sym"
	"go.uber.org/zap"
)

func setupHandlerWithWatcher(t *testing.T) (*CanvasHandler, *watcher.Engine, *storage.WatcherStore) {
	t.Helper()
	db := qntxtest.CreateTestDB(t)
	canvasStore := elementstorage.NewCanvasStore(db)
	logger := zap.NewNop().Sugar()
	engine := watcher.NewEngine(db, watcher.NewSQLReader(db), "http://localhost:8770", logger)
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

	// Create AX and Py elements with correct symbols
	items := []*elementstorage.CanvasElement{
		{ID: "ax-element-1", Symbol: sym.AX, X: 100, Y: 100},
		{ID: "py-element-1", Symbol: "py", X: 200, Y: 100},
	}
	for _, g := range items {
		if err := handler.store.UpsertElement(ctx, g); err != nil {
			t.Fatalf("UpsertElement failed: %v", err)
		}
	}

	// Create the AX element's watcher (as ax-element.ts would via WebSocket)
	axWatcher := &storage.Watcher{
		ID:                "ax-element-ax-element-1",
		Name:              "AX Element: contact",
		AxQuery:           "contact",
		ActionType:        storage.ActionTypePython,
		MaxFiresPerSecond: 60,
		Enabled:           true,
	}
	if err := watcherStore.Create(ctx, axWatcher); err != nil {
		t.Fatalf("Failed to create AX watcher: %v", err)
	}
	if err := engine.ReloadWatchers(); err != nil {
		t.Fatalf("ReloadWatchers failed: %v", err)
	}

	// Upsert composition with right edge: ax → py
	comp := elementstorage.CanvasComposition{
		ID: "comp-ax-py",
		Edges: []*pb.CompositionEdge{
			makeEdge("ax-element-1", "py-element-1", "right", 0),
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
	meldWatcher, err := watcherStore.Get(ctx, "meld-edge-comp-ax-py-ax-element-1-py-element-1")
	if err != nil {
		t.Fatalf("Meld-edge watcher not found: %v", err)
	}

	if meldWatcher.ActionType != storage.ActionTypeElementExecute {
		t.Errorf("Expected action type element_execute, got %s", meldWatcher.ActionType)
	}
	if meldWatcher.AxQuery != "contact" {
		t.Errorf("Expected AxQuery 'contact', got %q", meldWatcher.AxQuery)
	}

	// Verify action data has target element info
	var actionData map[string]string
	if err := json.Unmarshal([]byte(meldWatcher.ActionData), &actionData); err != nil {
		t.Fatalf("Failed to parse action data: %v", err)
	}
	if actionData["target_element_id"] != "py-element-1" {
		t.Errorf("Expected target_element_id 'py-element-1', got %q", actionData["target_element_id"])
	}
	if actionData["target_element_type"] != "py" {
		t.Errorf("Expected target_element_type 'py', got %q", actionData["target_element_type"])
	}
}

func TestCompileSubscriptions_PyToPrompt(t *testing.T) {
	handler, _, watcherStore := setupHandlerWithWatcher(t)
	ctx := context.Background()

	// Create Py and Prompt elements
	items := []*elementstorage.CanvasElement{
		{ID: "py-element-1", Symbol: "py", X: 100, Y: 100},
		{ID: "prompt-element-1", Symbol: sym.SO, X: 200, Y: 100},
	}
	for _, g := range items {
		if err := handler.store.UpsertElement(ctx, g); err != nil {
			t.Fatalf("UpsertElement failed: %v", err)
		}
	}

	// Upsert composition: py → prompt
	comp := elementstorage.CanvasComposition{
		ID: "comp-py-prompt",
		Edges: []*pb.CompositionEdge{
			makeEdge("py-element-1", "prompt-element-1", "right", 0),
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
	meldWatcher, err := watcherStore.Get(ctx, "meld-edge-comp-py-prompt-py-element-1-prompt-element-1")
	if err != nil {
		t.Fatalf("Meld-edge watcher not found: %v", err)
	}

	if meldWatcher.AxQuery != "" {
		t.Errorf("Producer edge should not have AxQuery, got %q", meldWatcher.AxQuery)
	}
	if len(meldWatcher.Filter.Actors) != 1 || meldWatcher.Filter.Actors[0] != "element:py-element-1" {
		t.Errorf("Expected actors filter [element:py-element-1], got %v", meldWatcher.Filter.Actors)
	}

	var actionData map[string]string
	if err := json.Unmarshal([]byte(meldWatcher.ActionData), &actionData); err != nil {
		t.Fatalf("Failed to parse action data: %v", err)
	}
	if actionData["target_element_type"] != "prompt" {
		t.Errorf("Expected target type 'prompt', got %q", actionData["target_element_type"])
	}
}

func TestCompileSubscriptions_DeleteCleansUpWatchers(t *testing.T) {
	handler, _, watcherStore := setupHandlerWithWatcher(t)
	ctx := context.Background()

	// Create elements
	items := []*elementstorage.CanvasElement{
		{ID: "py-a", Symbol: "py", X: 100, Y: 100},
		{ID: "py-b", Symbol: "py", X: 200, Y: 100},
	}
	for _, g := range items {
		if err := handler.store.UpsertElement(ctx, g); err != nil {
			t.Fatalf("UpsertElement failed: %v", err)
		}
	}

	// Create composition: py-a → py-b
	comp := elementstorage.CanvasComposition{
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

	// Create elements
	items := []*elementstorage.CanvasElement{
		{ID: "py-1", Symbol: "py", X: 100, Y: 100},
		{ID: "result-1", Symbol: "result", X: 100, Y: 200},
	}
	for _, g := range items {
		if err := handler.store.UpsertElement(ctx, g); err != nil {
			t.Fatalf("UpsertElement failed: %v", err)
		}
	}

	// Create composition with bottom edge only (result auto-meld)
	comp := elementstorage.CanvasComposition{
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
		if wt.ActionType == storage.ActionTypeElementExecute {
			t.Errorf("No element_execute watchers expected for bottom edges, found: %s", wt.ID)
		}
	}
}

func TestCompileSubscriptions_StaleEdgeCleanup(t *testing.T) {
	handler, engine, watcherStore := setupHandlerWithWatcher(t)
	ctx := context.Background()

	// Create AX and two Py elements
	items := []*elementstorage.CanvasElement{
		{ID: "ax-1", Symbol: sym.AX, X: 100, Y: 100},
		{ID: "py-a", Symbol: "py", X: 200, Y: 100},
		{ID: "py-b", Symbol: "py", X: 300, Y: 100},
	}
	for _, g := range items {
		if err := handler.store.UpsertElement(ctx, g); err != nil {
			t.Fatalf("UpsertElement failed: %v", err)
		}
	}

	// Create AX watcher (as ax-element.ts would)
	axWatcher := &storage.Watcher{
		ID:                "ax-element-ax-1",
		Name:              "AX Element: contact",
		AxQuery:           "contact",
		ActionType:        storage.ActionTypePython,
		MaxFiresPerSecond: 60,
		Enabled:           true,
	}
	if err := watcherStore.Create(ctx, axWatcher); err != nil {
		t.Fatalf("Failed to create AX watcher: %v", err)
	}
	if err := engine.ReloadWatchers(); err != nil {
		t.Fatalf("ReloadWatchers failed: %v", err)
	}

	// POST composition with 2 right edges: ax→py-a, ax→py-b
	comp := elementstorage.CanvasComposition{
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
