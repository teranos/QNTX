package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

// Helper function to create edges for testing
func makeEdge(from, to, direction string, position int32) *pb.CompositionEdge {
	return &pb.CompositionEdge{
		From:      from,
		To:        to,
		Direction: direction,
		Position:  position,
	}
}

func TestCanvasHandler_HandleElements_POST(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	item := elementstorage.CanvasElement{
		ID:     "element-1",
		Symbol: "🜶",
		X:      100,
		Y:      200,
	}

	body, _ := json.Marshal(item)
	req := httptest.NewRequest(http.MethodPost, "/api/canvas/elements", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleElements(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response elementstorage.CanvasElement
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.ID != item.ID {
		t.Errorf("ID mismatch: got %s, want %s", response.ID, item.ID)
	}
	if response.Symbol != item.Symbol {
		t.Errorf("Symbol mismatch: got %s, want %s", response.Symbol, item.Symbol)
	}
}

func TestCanvasHandler_HandleElements_GET_List(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	// Create test elements
	items := []*elementstorage.CanvasElement{
		{ID: "element-1", Symbol: "🜶", X: 100, Y: 100},
		{ID: "element-2", Symbol: "🝓", X: 200, Y: 200},
	}

	for _, g := range items {
		if err := store.UpsertElement(context.Background(), g); err != nil {
			t.Fatalf("UpsertElement failed: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/canvas/elements", nil)
	w := httptest.NewRecorder()

	handler.HandleElements(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response []*elementstorage.CanvasElement
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(response) != 2 {
		t.Errorf("Expected 2 elements, got %d", len(response))
	}
}

func TestCanvasHandler_HandleElements_GET_Single(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	item := &elementstorage.CanvasElement{
		ID:     "element-1",
		Symbol: "🝗",
		X:      100,
		Y:      200,
	}

	if err := store.UpsertElement(context.Background(), item); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/canvas/elements/element-1", nil)
	w := httptest.NewRecorder()

	handler.HandleElements(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response elementstorage.CanvasElement
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.ID != item.ID {
		t.Errorf("ID mismatch: got %s, want %s", response.ID, item.ID)
	}
}

func TestCanvasHandler_HandleElements_GET_NotFound(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/canvas/elements/nonexistent", nil)
	w := httptest.NewRecorder()

	handler.HandleElements(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestCanvasHandler_HandleElements_DELETE(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	item := &elementstorage.CanvasElement{
		ID:     "element-1",
		Symbol: "🜶",
		X:      100,
		Y:      200,
	}

	if err := store.UpsertElement(context.Background(), item); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/canvas/elements/element-1", nil)
	w := httptest.NewRecorder()

	handler.HandleElements(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", w.Code)
	}

	// Verify element was deleted
	_, err := store.GetElement(context.Background(), "element-1")
	if err == nil {
		t.Error("Expected error when getting deleted element, got nil")
	}
}

func TestCanvasHandler_HandleElements_DELETE_NotFound(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodDelete, "/api/canvas/elements/nonexistent", nil)
	w := httptest.NewRecorder()

	handler.HandleElements(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestCanvasHandler_HandleElements_DELETE_MissingID(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodDelete, "/api/canvas/elements", nil)
	w := httptest.NewRecorder()

	handler.HandleElements(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestCanvasHandler_HandleElements_InvalidMethod(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodPut, "/api/canvas/elements", nil)
	w := httptest.NewRecorder()

	handler.HandleElements(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestCanvasHandler_HandleCompositions_POST(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	// Create elements first (foreign key requirement)
	if err := store.UpsertElement(context.Background(), &elementstorage.CanvasElement{ID: "element-1", Symbol: "🜶", X: 100, Y: 100}); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}
	if err := store.UpsertElement(context.Background(), &elementstorage.CanvasElement{ID: "element-2", Symbol: "🝓", X: 200, Y: 200}); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}

	comp := elementstorage.CanvasComposition{
		ID: "comp-1",
		Edges: []*pb.CompositionEdge{
			makeEdge("element-1", "element-2", "right", 0),
		},
		X: 150,
		Y: 150,
	}

	body, _ := json.Marshal(comp)
	req := httptest.NewRequest(http.MethodPost, "/api/canvas/compositions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleCompositions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response elementstorage.CanvasComposition
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.ID != comp.ID {
		t.Errorf("ID mismatch: got %s, want %s", response.ID, comp.ID)
	}
	if len(response.Edges) != 1 {
		t.Errorf("Expected 1 edge, got %d", len(response.Edges))
	}
}

func TestCanvasHandler_HandleCompositions_GET_List(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	// Create elements first (foreign key requirement)
	if err := store.UpsertElement(context.Background(), &elementstorage.CanvasElement{ID: "g1", Symbol: "🜶", X: 100, Y: 100}); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}
	if err := store.UpsertElement(context.Background(), &elementstorage.CanvasElement{ID: "g2", Symbol: "🝓", X: 200, Y: 200}); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}
	if err := store.UpsertElement(context.Background(), &elementstorage.CanvasElement{ID: "g3", Symbol: "🝗", X: 300, Y: 300}); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}

	comps := []*elementstorage.CanvasComposition{
		{
			ID:    "comp-1",
			Edges: []*pb.CompositionEdge{makeEdge("g1", "g2", "right", 0)},
			X:     100,
			Y:     100,
		},
		{
			ID:    "comp-2",
			Edges: []*pb.CompositionEdge{makeEdge("g2", "g3", "right", 0)},
			X:     200,
			Y:     200,
		},
	}

	for _, c := range comps {
		if err := store.UpsertComposition(context.Background(), c); err != nil {
			t.Fatalf("UpsertComposition failed: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/canvas/compositions", nil)
	w := httptest.NewRecorder()

	handler.HandleCompositions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response []*elementstorage.CanvasComposition
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(response) != 2 {
		t.Errorf("Expected 2 compositions, got %d", len(response))
	}
}

func TestCanvasHandler_HandleCompositions_GET_Single(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	// Create elements first (foreign key requirement)
	if err := store.UpsertElement(context.Background(), &elementstorage.CanvasElement{ID: "element-1", Symbol: "🜶", X: 100, Y: 100}); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}
	if err := store.UpsertElement(context.Background(), &elementstorage.CanvasElement{ID: "element-2", Symbol: "🝓", X: 200, Y: 200}); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}

	comp := &elementstorage.CanvasComposition{
		ID: "comp-1",
		Edges: []*pb.CompositionEdge{
			makeEdge("element-1", "element-2", "right", 0),
		},
		X: 100,
		Y: 200,
	}

	if err := store.UpsertComposition(context.Background(), comp); err != nil {
		t.Fatalf("UpsertComposition failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/canvas/compositions/comp-1", nil)
	w := httptest.NewRecorder()

	handler.HandleCompositions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response elementstorage.CanvasComposition
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.ID != comp.ID {
		t.Errorf("ID mismatch: got %s, want %s", response.ID, comp.ID)
	}
}

func TestCanvasHandler_HandleCompositions_GET_NotFound(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/canvas/compositions/nonexistent", nil)
	w := httptest.NewRecorder()

	handler.HandleCompositions(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestCanvasHandler_HandleCompositions_DELETE(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	// Create elements first (foreign key requirement)
	if err := store.UpsertElement(context.Background(), &elementstorage.CanvasElement{ID: "element-1", Symbol: "🜶", X: 100, Y: 100}); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}
	if err := store.UpsertElement(context.Background(), &elementstorage.CanvasElement{ID: "element-2", Symbol: "🝓", X: 200, Y: 200}); err != nil {
		t.Fatalf("UpsertElement failed: %v", err)
	}

	comp := &elementstorage.CanvasComposition{
		ID: "comp-1",
		Edges: []*pb.CompositionEdge{
			makeEdge("element-1", "element-2", "right", 0),
		},
		X: 100,
		Y: 200,
	}

	if err := store.UpsertComposition(context.Background(), comp); err != nil {
		t.Fatalf("UpsertComposition failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/canvas/compositions/comp-1", nil)
	w := httptest.NewRecorder()

	handler.HandleCompositions(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", w.Code)
	}

	// Verify composition was deleted
	_, err := store.GetComposition(context.Background(), "comp-1")
	if err == nil {
		t.Error("Expected error when getting deleted composition, got nil")
	}
}

func TestCanvasHandler_HandleCompositions_DELETE_NotFound(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodDelete, "/api/canvas/compositions/nonexistent", nil)
	w := httptest.NewRecorder()

	handler.HandleCompositions(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestCanvasHandler_HandleCompositions_InvalidMethod(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodPut, "/api/canvas/compositions", nil)
	w := httptest.NewRecorder()

	handler.HandleCompositions(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestCanvasHandler_HandleElements_POST_InvalidJSON(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/api/canvas/elements", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleElements(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestCanvasHandler_HandleCompositions_POST_InvalidJSON(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/api/canvas/compositions", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleCompositions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

// TODO(#441): Phase 2-5 - Multi-element chain backend tests
func TestCanvasHandler_HandleCompositions_POST_ThreeElements(t *testing.T) {
	t.Skip("TODO(#441): Unskip when 3-element chain UI is implemented")

	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	// Create three elements (ax, py, prompt)
	items := []*elementstorage.CanvasElement{
		{ID: "ax-1", Symbol: "🜶", X: 100, Y: 100},
		{ID: "py-1", Symbol: "🝓", X: 200, Y: 100},
		{ID: "prompt-1", Symbol: "🝗", X: 300, Y: 100},
	}
	for _, g := range items {
		if err := store.UpsertElement(context.Background(), g); err != nil {
			t.Fatalf("UpsertElement failed: %v", err)
		}
	}

	// Create 3-element composition (ax|py|prompt) - linear chain with 2 edges
	comp := elementstorage.CanvasComposition{
		ID: "comp-ax-py-prompt",
		Edges: []*pb.CompositionEdge{
			makeEdge("ax-1", "py-1", "right", 0),
			makeEdge("py-1", "prompt-1", "right", 1),
		},
		X: 150,
		Y: 150,
	}

	body, _ := json.Marshal(comp)
	req := httptest.NewRequest(http.MethodPost, "/api/canvas/compositions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleCompositions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var response elementstorage.CanvasComposition
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify 2 edges returned (3-element chain = 2 edges)
	if len(response.Edges) != 2 {
		t.Errorf("Expected 2 edges, got %d", len(response.Edges))
	}

	// Verify correct edge structure preserved
	if response.Edges[0].From != "ax-1" || response.Edges[0].To != "py-1" {
		t.Errorf("First edge incorrect: got %s→%s, want ax-1→py-1", response.Edges[0].From, response.Edges[0].To)
	}
	if response.Edges[1].From != "py-1" || response.Edges[1].To != "prompt-1" {
		t.Errorf("Second edge incorrect: got %s→%s, want py-1→prompt-1", response.Edges[1].From, response.Edges[1].To)
	}
}

func TestCanvasHandler_HandleCompositions_GET_PreservesElementOrder(t *testing.T) {
	t.Skip("TODO(#441): Unskip when 3-element chain UI is implemented")

	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	// Create elements
	items := []*elementstorage.CanvasElement{
		{ID: "ax-1", Symbol: "🜶", X: 100, Y: 100},
		{ID: "py-1", Symbol: "🝓", X: 200, Y: 100},
		{ID: "prompt-1", Symbol: "🝗", X: 300, Y: 100},
	}
	for _, g := range items {
		if err := store.UpsertElement(context.Background(), g); err != nil {
			t.Fatalf("UpsertElement failed: %v", err)
		}
	}

	// Create 3-element composition directly in storage
	comp := &elementstorage.CanvasComposition{
		ID: "comp-1",
		Edges: []*pb.CompositionEdge{
			makeEdge("ax-1", "py-1", "right", 0),
			makeEdge("py-1", "prompt-1", "right", 1),
		},
		X: 150,
		Y: 150,
	}
	if err := store.UpsertComposition(context.Background(), comp); err != nil {
		t.Fatalf("UpsertComposition failed: %v", err)
	}

	// GET single composition
	req := httptest.NewRequest(http.MethodGet, "/api/canvas/compositions/comp-1", nil)
	w := httptest.NewRecorder()

	handler.HandleCompositions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response elementstorage.CanvasComposition
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Critical: Verify edge order is preserved (left-to-right chain: ax→py→prompt)
	if len(response.Edges) != 2 {
		t.Fatalf("Expected 2 edges (3-element chain), got %d", len(response.Edges))
	}

	// Verify edges maintain position order
	if response.Edges[0].Position != 0 || response.Edges[1].Position != 1 {
		t.Errorf("Edge positions not preserved: got %d, %d; want 0, 1",
			response.Edges[0].Position, response.Edges[1].Position)
	}

	// Verify edge structure
	if response.Edges[0].From != "ax-1" || response.Edges[0].To != "py-1" {
		t.Errorf("First edge wrong: got %s→%s, want ax-1→py-1",
			response.Edges[0].From, response.Edges[0].To)
	}
	if response.Edges[1].From != "py-1" || response.Edges[1].To != "prompt-1" {
		t.Errorf("Second edge wrong: got %s→%s, want py-1→prompt-1",
			response.Edges[1].From, response.Edges[1].To)
	}
}

func TestCanvasHandler_HandleCompositions_POST_FourElementChain(t *testing.T) {
	t.Skip("TODO(#441): Unskip when N-element chain extension is implemented")

	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	// Create four elements
	items := []*elementstorage.CanvasElement{
		{ID: "ax-1", Symbol: "🜶", X: 100, Y: 100},
		{ID: "py-1", Symbol: "🝓", X: 200, Y: 100},
		{ID: "prompt-1", Symbol: "🝗", X: 300, Y: 100},
		{ID: "prompt-2", Symbol: "🝗", X: 400, Y: 100},
	}
	for _, g := range items {
		if err := store.UpsertElement(context.Background(), g); err != nil {
			t.Fatalf("UpsertElement failed: %v", err)
		}
	}

	// Create 4-element composition (3 edges: ax→py→prompt1→prompt2)
	comp := elementstorage.CanvasComposition{
		ID: "comp-4-chain",
		Edges: []*pb.CompositionEdge{
			makeEdge("ax-1", "py-1", "right", 0),
			makeEdge("py-1", "prompt-1", "right", 1),
			makeEdge("prompt-1", "prompt-2", "right", 2),
		},
		X: 150,
		Y: 150,
	}

	body, _ := json.Marshal(comp)
	req := httptest.NewRequest(http.MethodPost, "/api/canvas/compositions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleCompositions(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var response elementstorage.CanvasComposition
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify 4-element chain stored and retrieved correctly (3 edges)
	if len(response.Edges) != 3 {
		t.Errorf("Expected 3 edges (4-element chain), got %d", len(response.Edges))
	}

	// Verify edge structure and order
	expectedEdges := [][2]string{
		{"ax-1", "py-1"},
		{"py-1", "prompt-1"},
		{"prompt-1", "prompt-2"},
	}
	for i, expected := range expectedEdges {
		if response.Edges[i].From != expected[0] || response.Edges[i].To != expected[1] {
			t.Errorf("Edge[%d]: expected %s→%s, got %s→%s", i,
				expected[0], expected[1], response.Edges[i].From, response.Edges[i].To)
		}
	}
}

// === Minimized window handler tests ===

func TestCanvasHandler_HandleMinimizedWindows_POST(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	body := []byte(`{"element_id": "element-1"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/canvas/minimized-windows", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleMinimizedWindows(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected status 201, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCanvasHandler_HandleMinimizedWindows_POST_MissingElementID(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/canvas/minimized-windows", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleMinimizedWindows(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestCanvasHandler_HandleMinimizedWindows_GET(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	// Add two minimized windows
	if err := store.AddMinimizedWindow(context.Background(), "element-1"); err != nil {
		t.Fatalf("AddMinimizedWindow failed: %v", err)
	}
	if err := store.AddMinimizedWindow(context.Background(), "element-2"); err != nil {
		t.Fatalf("AddMinimizedWindow failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/canvas/minimized-windows", nil)
	w := httptest.NewRecorder()

	handler.HandleMinimizedWindows(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response []elementstorage.MinimizedWindow
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(response) != 2 {
		t.Errorf("Expected 2 minimized windows, got %d", len(response))
	}
}

func TestCanvasHandler_HandleMinimizedWindows_DELETE(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	if err := store.AddMinimizedWindow(context.Background(), "element-1"); err != nil {
		t.Fatalf("AddMinimizedWindow failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/canvas/minimized-windows/element-1", nil)
	w := httptest.NewRecorder()

	handler.HandleMinimizedWindows(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", w.Code)
	}
}

func TestCanvasHandler_HandleMinimizedWindows_DELETE_NotFound(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodDelete, "/api/canvas/minimized-windows/nonexistent", nil)
	w := httptest.NewRecorder()

	handler.HandleMinimizedWindows(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestCanvasHandler_HandleMinimizedWindows_DELETE_MissingID(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodDelete, "/api/canvas/minimized-windows", nil)
	w := httptest.NewRecorder()

	handler.HandleMinimizedWindows(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestCanvasHandler_HandleMinimizedWindows_InvalidMethod(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := elementstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodPut, "/api/canvas/minimized-windows", nil)
	w := httptest.NewRecorder()

	handler.HandleMinimizedWindows(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestElementSymbolToType(t *testing.T) {
	tests := []struct {
		symbol   string
		expected string
	}{
		{"py", "py"},
		{sym.AX, "ax"},       // ⋈ → ax
		{sym.SE, "semantic"}, // ⊨ → semantic
		{sym.SO, "prompt"},   // ⟶ → prompt
		{"note", "note"},     // Unknown passes through
		{"result", "result"},
	}

	for _, tt := range tests {
		got := elementSymbolToType(tt.symbol)
		if got != tt.expected {
			t.Errorf("elementSymbolToType(%q) = %q, want %q", tt.symbol, got, tt.expected)
		}
	}
}

// === SE→SE meldability tests ===

// setupSEtoSE creates two SE elements with standalone watchers and returns a handler
// with watcher engine wired up, ready for compileSubscriptions testing.
func setupSEtoSE(t *testing.T) (*CanvasHandler, storage.Watchers, context.Context) {
	t.Helper()
	db := qntxtest.CreateTestDB(t)
	canvasStore := elementstorage.NewCanvasStore(db)
	logger := zap.NewNop().Sugar()
	engine := watcher.NewEngine(db, watcher.NewSQLReader(db), "http://localhost:8770", logger)
	if err := engine.Start(); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	t.Cleanup(engine.Stop)

	handler := NewCanvasHandler(canvasStore, WithWatcherEngine(engine, logger))
	watcherStore := engine.GetStore()
	ctx := context.Background()

	// Create SE₁ and SE₂ elements
	if err := canvasStore.UpsertElement(ctx, &elementstorage.CanvasElement{
		ID: "se-1", Symbol: sym.SE, X: 100, Y: 100,
	}); err != nil {
		t.Fatalf("UpsertElement SE₁ failed: %v", err)
	}
	if err := canvasStore.UpsertElement(ctx, &elementstorage.CanvasElement{
		ID: "se-2", Symbol: sym.SE, X: 300, Y: 100,
	}); err != nil {
		t.Fatalf("UpsertElement SE₂ failed: %v", err)
	}

	// Create standalone watchers (simulating what the frontend does)
	se1Watcher := &storage.Watcher{
		ID:                "se-element-se-1",
		Name:              "SE: science",
		ActionType:        storage.ActionTypeSemanticMatch,
		SemanticQuery:     "science",
		SemanticThreshold: 0.4,
		MaxFiresPerSecond: 60,
		Enabled:           true,
	}
	se2Watcher := &storage.Watcher{
		ID:                "se-element-se-2",
		Name:              "SE: about teaching",
		ActionType:        storage.ActionTypeSemanticMatch,
		SemanticQuery:     "about teaching",
		SemanticThreshold: 0.5,
		MaxFiresPerSecond: 60,
		Enabled:           true,
	}
	if err := watcherStore.Create(ctx, se1Watcher); err != nil {
		t.Fatalf("Create SE₁ watcher failed: %v", err)
	}
	if err := watcherStore.Create(ctx, se2Watcher); err != nil {
		t.Fatalf("Create SE₂ watcher failed: %v", err)
	}

	return handler, watcherStore, ctx
}

func TestCompileSubscriptions_SEtoSE_CreatesCompoundAndDisablesDownstream(t *testing.T) {
	handler, watcherStore, ctx := setupSEtoSE(t)

	// Create composition with SE₁→SE₂ edge
	comp := &elementstorage.CanvasComposition{
		ID: "comp-se-se",
		Edges: []*pb.CompositionEdge{
			makeEdge("se-1", "se-2", "right", 0),
		},
		X: 200, Y: 100,
	}
	if err := handler.store.UpsertComposition(ctx, comp); err != nil {
		t.Fatalf("UpsertComposition failed: %v", err)
	}

	// Compile subscriptions
	if err := handler.compileSubscriptions(ctx, comp); err != nil {
		t.Fatalf("compileSubscriptions failed: %v", err)
	}

	// 1. Compound meld-edge watcher should exist with both queries
	compoundID := fmt.Sprintf("meld-edge-%s-%s-%s", comp.ID, "se-1", "se-2")
	compound, err := watcherStore.Get(ctx, compoundID)
	if err != nil {
		t.Fatalf("Compound watcher not created: %v", err)
	}
	if compound.SemanticQuery != "about teaching" {
		t.Errorf("Compound downstream query wrong: got %q, want %q", compound.SemanticQuery, "about teaching")
	}
	if compound.UpstreamSemanticQuery != "science" {
		t.Errorf("Compound upstream query wrong: got %q, want %q", compound.UpstreamSemanticQuery, "science")
	}

	// 2. SE₂'s standalone watcher stays enabled in DB (engine-level suppression)
	se2, err := watcherStore.Get(ctx, "se-element-se-2")
	if err != nil {
		t.Fatalf("Get SE₂ watcher failed: %v", err)
	}
	if !se2.Enabled {
		t.Error("SE₂ standalone watcher should stay enabled in DB (engine suppresses at load time)")
	}

	// 3. SE₁'s standalone watcher should remain enabled
	se1, err := watcherStore.Get(ctx, "se-element-se-1")
	if err != nil {
		t.Fatalf("Get SE₁ watcher failed: %v", err)
	}
	if !se1.Enabled {
		t.Error("SE₁ standalone watcher should remain enabled")
	}
}

func TestCompileSubscriptions_SEtoSE_EngineSuppressesStandalone(t *testing.T) {
	handler, watcherStore, ctx := setupSEtoSE(t)

	// Create and compile SE₁→SE₂
	comp := &elementstorage.CanvasComposition{
		ID: "comp-se-se",
		Edges: []*pb.CompositionEdge{
			makeEdge("se-1", "se-2", "right", 0),
		},
		X: 200, Y: 100,
	}
	if err := handler.store.UpsertComposition(ctx, comp); err != nil {
		t.Fatalf("UpsertComposition failed: %v", err)
	}
	if err := handler.compileSubscriptions(ctx, comp); err != nil {
		t.Fatalf("compileSubscriptions failed: %v", err)
	}

	// Verify SE₂ stays enabled in DB (engine handles suppression, not DB)
	se2, err := watcherStore.Get(ctx, "se-element-se-2")
	if err != nil {
		t.Fatalf("Get SE₂ failed: %v", err)
	}
	if !se2.Enabled {
		t.Fatal("SE₂ should stay enabled in DB (engine-level suppression)")
	}

	// Verify compound watcher has both queries
	compoundID := fmt.Sprintf("meld-edge-%s-%s-%s", comp.ID, "se-1", "se-2")
	compound, err := watcherStore.Get(ctx, compoundID)
	if err != nil {
		t.Fatalf("Compound watcher not created: %v", err)
	}
	if compound.SemanticQuery != "about teaching" {
		t.Errorf("Compound downstream query wrong: got %q, want %q", compound.SemanticQuery, "about teaching")
	}
	if compound.UpstreamSemanticQuery != "science" {
		t.Errorf("Compound upstream query wrong: got %q, want %q", compound.UpstreamSemanticQuery, "science")
	}

	// After engine reload, SE₂ standalone should be suppressed in engine
	// (not in the engine's in-memory map) because compound watcher targets it.
	// We verify via GetWatcher — SE₂ should not be loaded.
	engine := handler.watcherEngine
	_, se2InEngine := engine.GetWatcher("se-element-se-2")
	if se2InEngine {
		t.Error("SE₂ standalone should be suppressed in engine when compound target exists")
	}

	// SE₁ should still be in the engine
	_, se1InEngine := engine.GetWatcher("se-element-se-1")
	if !se1InEngine {
		t.Error("SE₁ standalone should remain in engine")
	}

	// Compound watcher should be in the engine
	_, compoundInEngine := engine.GetWatcher(compoundID)
	if !compoundInEngine {
		t.Error("Compound watcher should be loaded in engine")
	}
}

func TestCompileSubscriptions_SEtoSEtoPrompt_PropagatesUpstream(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	canvasStore := elementstorage.NewCanvasStore(db)
	logger := zap.NewNop().Sugar()
	engine := watcher.NewEngine(db, watcher.NewSQLReader(db), "http://localhost:8770", logger)
	if err := engine.Start(); err != nil {
		t.Fatalf("Engine start failed: %v", err)
	}
	t.Cleanup(engine.Stop)

	handler := NewCanvasHandler(canvasStore, WithWatcherEngine(engine, logger))
	watcherStore := engine.GetStore()
	ctx := context.Background()

	// Create SE₁, SE₂, and prompt elements
	for _, g := range []*elementstorage.CanvasElement{
		{ID: "se-1", Symbol: sym.SE, X: 100, Y: 100},
		{ID: "se-2", Symbol: sym.SE, X: 300, Y: 100},
		{ID: "prompt-1", Symbol: sym.SO, X: 500, Y: 100},
	} {
		if err := canvasStore.UpsertElement(ctx, g); err != nil {
			t.Fatalf("UpsertElement %s failed: %v", g.ID, err)
		}
	}

	// Create standalone watchers
	for _, w := range []*storage.Watcher{
		{ID: "se-element-se-1", Name: "SE: science", ActionType: storage.ActionTypeSemanticMatch, SemanticQuery: "science", SemanticThreshold: 0.4, MaxFiresPerSecond: 60, Enabled: true},
		{ID: "se-element-se-2", Name: "SE: teaching", ActionType: storage.ActionTypeSemanticMatch, SemanticQuery: "about teaching", SemanticThreshold: 0.5, MaxFiresPerSecond: 60, Enabled: true},
	} {
		if err := watcherStore.Create(ctx, w); err != nil {
			t.Fatalf("Create watcher %s failed: %v", w.ID, err)
		}
	}

	// Create composition: SE₁ → SE₂ → prompt
	comp := &elementstorage.CanvasComposition{
		ID: "comp-se-se-prompt",
		Edges: []*pb.CompositionEdge{
			makeEdge("se-1", "se-2", "right", 0),
			makeEdge("se-2", "prompt-1", "right", 1),
		},
		X: 300, Y: 100,
	}
	if err := canvasStore.UpsertComposition(ctx, comp); err != nil {
		t.Fatalf("UpsertComposition failed: %v", err)
	}

	// Compile subscriptions
	if err := handler.compileSubscriptions(ctx, comp); err != nil {
		t.Fatalf("compileSubscriptions failed: %v", err)
	}

	// The SE₂→prompt watcher should carry SE₁'s upstream query
	// so the prompt only executes for attestations matching BOTH queries
	promptWatcherID := fmt.Sprintf("meld-edge-%s-%s-%s", comp.ID, "se-2", "prompt-1")
	promptWatcher, err := watcherStore.Get(ctx, promptWatcherID)
	if err != nil {
		t.Fatalf("SE₂→prompt watcher not created: %v", err)
	}

	// Downstream query = SE₂'s query
	if promptWatcher.SemanticQuery != "about teaching" {
		t.Errorf("SE₂→prompt downstream query wrong: got %q, want %q", promptWatcher.SemanticQuery, "about teaching")
	}

	// Upstream query = SE₁'s query (propagated from compound meld)
	if promptWatcher.UpstreamSemanticQuery != "science" {
		t.Errorf("SE₂→prompt upstream query not propagated: got %q, want %q", promptWatcher.UpstreamSemanticQuery, "science")
	}
	if promptWatcher.UpstreamSemanticThreshold != 0.4 {
		t.Errorf("SE₂→prompt upstream threshold not propagated: got %f, want 0.4", promptWatcher.UpstreamSemanticThreshold)
	}
}

func TestCompileSubscriptions_SEtoSE_UnmeldRestoresEngineState(t *testing.T) {
	handler, watcherStore, ctx := setupSEtoSE(t)

	// Create and compile SE₁→SE₂
	comp := &elementstorage.CanvasComposition{
		ID: "comp-se-se",
		Edges: []*pb.CompositionEdge{
			makeEdge("se-1", "se-2", "right", 0),
		},
		X: 200, Y: 100,
	}
	if err := handler.store.UpsertComposition(ctx, comp); err != nil {
		t.Fatalf("UpsertComposition failed: %v", err)
	}
	if err := handler.compileSubscriptions(ctx, comp); err != nil {
		t.Fatalf("compileSubscriptions failed: %v", err)
	}

	// Verify SE₂ is suppressed in engine
	engine := handler.watcherEngine
	_, se2InEngine := engine.GetWatcher("se-element-se-2")
	if se2InEngine {
		t.Fatal("SE₂ should be suppressed in engine while melded")
	}

	// Delete composition (unmeld)
	req := httptest.NewRequest(http.MethodDelete, "/api/canvas/compositions/comp-se-se", nil)
	w := httptest.NewRecorder()
	handler.HandleCompositions(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("Delete composition failed: status %d", w.Code)
	}

	// After unmeld, SE₂ should be back in the engine (no compound to suppress it)
	_, se2InEngineAfter := engine.GetWatcher("se-element-se-2")
	if !se2InEngineAfter {
		t.Error("SE₂ standalone should be restored in engine after unmeld")
	}

	// SE₂ is still enabled in DB (was never disabled)
	se2, err := watcherStore.Get(ctx, "se-element-se-2")
	if err != nil {
		t.Fatalf("Get SE₂ failed: %v", err)
	}
	if !se2.Enabled {
		t.Error("SE₂ should remain enabled in DB")
	}
}
