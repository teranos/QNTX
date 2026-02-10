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
	glyphstorage "github.com/teranos/QNTX/glyph/storage"
	pb "github.com/teranos/QNTX/glyph/proto"
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

func TestCanvasHandler_HandleGlyphs_POST(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	glyph := glyphstorage.CanvasGlyph{
		ID:     "glyph-1",
		Symbol: "🜶",
		X:      100,
		Y:      200,
	}

	body, _ := json.Marshal(glyph)
	req := httptest.NewRequest(http.MethodPost, "/api/canvas/glyphs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleGlyphs(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response glyphstorage.CanvasGlyph
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.ID != glyph.ID {
		t.Errorf("ID mismatch: got %s, want %s", response.ID, glyph.ID)
	}
	if response.Symbol != glyph.Symbol {
		t.Errorf("Symbol mismatch: got %s, want %s", response.Symbol, glyph.Symbol)
	}
}

func TestCanvasHandler_HandleGlyphs_GET_List(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	// Create test glyphs
	glyphs := []*glyphstorage.CanvasGlyph{
		{ID: "glyph-1", Symbol: "🜶", X: 100, Y: 100},
		{ID: "glyph-2", Symbol: "🝓", X: 200, Y: 200},
	}

	for _, g := range glyphs {
		if err := store.UpsertGlyph(context.Background(), g); err != nil {
			t.Fatalf("UpsertGlyph failed: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/canvas/glyphs", nil)
	w := httptest.NewRecorder()

	handler.HandleGlyphs(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response []*glyphstorage.CanvasGlyph
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(response) != 2 {
		t.Errorf("Expected 2 glyphs, got %d", len(response))
	}
}

func TestCanvasHandler_HandleGlyphs_GET_Single(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	glyph := &glyphstorage.CanvasGlyph{
		ID:     "glyph-1",
		Symbol: "🝗",
		X:      100,
		Y:      200,
	}

	if err := store.UpsertGlyph(context.Background(), glyph); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/canvas/glyphs/glyph-1", nil)
	w := httptest.NewRecorder()

	handler.HandleGlyphs(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response glyphstorage.CanvasGlyph
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.ID != glyph.ID {
		t.Errorf("ID mismatch: got %s, want %s", response.ID, glyph.ID)
	}
}

func TestCanvasHandler_HandleGlyphs_GET_NotFound(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/canvas/glyphs/nonexistent", nil)
	w := httptest.NewRecorder()

	handler.HandleGlyphs(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestCanvasHandler_HandleGlyphs_DELETE(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	glyph := &glyphstorage.CanvasGlyph{
		ID:     "glyph-1",
		Symbol: "🜶",
		X:      100,
		Y:      200,
	}

	if err := store.UpsertGlyph(context.Background(), glyph); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/canvas/glyphs/glyph-1", nil)
	w := httptest.NewRecorder()

	handler.HandleGlyphs(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("Expected status 204, got %d", w.Code)
	}

	// Verify glyph was deleted
	_, err := store.GetGlyph(context.Background(), "glyph-1")
	if err == nil {
		t.Error("Expected error when getting deleted glyph, got nil")
	}
}

func TestCanvasHandler_HandleGlyphs_DELETE_NotFound(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodDelete, "/api/canvas/glyphs/nonexistent", nil)
	w := httptest.NewRecorder()

	handler.HandleGlyphs(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", w.Code)
	}
}

func TestCanvasHandler_HandleGlyphs_DELETE_MissingID(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodDelete, "/api/canvas/glyphs", nil)
	w := httptest.NewRecorder()

	handler.HandleGlyphs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestCanvasHandler_HandleGlyphs_InvalidMethod(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodPut, "/api/canvas/glyphs", nil)
	w := httptest.NewRecorder()

	handler.HandleGlyphs(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestCanvasHandler_HandleCompositions_POST(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	// Create glyphs first (foreign key requirement)
	if err := store.UpsertGlyph(context.Background(), &glyphstorage.CanvasGlyph{ID: "glyph-1", Symbol: "🜶", X: 100, Y: 100}); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}
	if err := store.UpsertGlyph(context.Background(), &glyphstorage.CanvasGlyph{ID: "glyph-2", Symbol: "🝓", X: 200, Y: 200}); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}

	comp := glyphstorage.CanvasComposition{
		ID: "comp-1",
		Edges: []*pb.CompositionEdge{
			makeEdge("glyph-1", "glyph-2", "right", 0),
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

	var response glyphstorage.CanvasComposition
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
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	// Create glyphs first (foreign key requirement)
	if err := store.UpsertGlyph(context.Background(), &glyphstorage.CanvasGlyph{ID: "g1", Symbol: "🜶", X: 100, Y: 100}); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}
	if err := store.UpsertGlyph(context.Background(), &glyphstorage.CanvasGlyph{ID: "g2", Symbol: "🝓", X: 200, Y: 200}); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}
	if err := store.UpsertGlyph(context.Background(), &glyphstorage.CanvasGlyph{ID: "g3", Symbol: "🝗", X: 300, Y: 300}); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}

	comps := []*glyphstorage.CanvasComposition{
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

	var response []*glyphstorage.CanvasComposition
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(response) != 2 {
		t.Errorf("Expected 2 compositions, got %d", len(response))
	}
}

func TestCanvasHandler_HandleCompositions_GET_Single(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	// Create glyphs first (foreign key requirement)
	if err := store.UpsertGlyph(context.Background(), &glyphstorage.CanvasGlyph{ID: "glyph-1", Symbol: "🜶", X: 100, Y: 100}); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}
	if err := store.UpsertGlyph(context.Background(), &glyphstorage.CanvasGlyph{ID: "glyph-2", Symbol: "🝓", X: 200, Y: 200}); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}

	comp := &glyphstorage.CanvasComposition{
		ID: "comp-1",
		Edges: []*pb.CompositionEdge{
			makeEdge("glyph-1", "glyph-2", "right", 0),
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

	var response glyphstorage.CanvasComposition
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if response.ID != comp.ID {
		t.Errorf("ID mismatch: got %s, want %s", response.ID, comp.ID)
	}
}

func TestCanvasHandler_HandleCompositions_GET_NotFound(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
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
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	// Create glyphs first (foreign key requirement)
	if err := store.UpsertGlyph(context.Background(), &glyphstorage.CanvasGlyph{ID: "glyph-1", Symbol: "🜶", X: 100, Y: 100}); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}
	if err := store.UpsertGlyph(context.Background(), &glyphstorage.CanvasGlyph{ID: "glyph-2", Symbol: "🝓", X: 200, Y: 200}); err != nil {
		t.Fatalf("UpsertGlyph failed: %v", err)
	}

	comp := &glyphstorage.CanvasComposition{
		ID: "comp-1",
		Edges: []*pb.CompositionEdge{
			makeEdge("glyph-1", "glyph-2", "right", 0),
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
	store := glyphstorage.NewCanvasStore(db)
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
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodPut, "/api/canvas/compositions", nil)
	w := httptest.NewRecorder()

	handler.HandleCompositions(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", w.Code)
	}
}

func TestCanvasHandler_HandleGlyphs_POST_InvalidJSON(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/api/canvas/glyphs", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleGlyphs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

func TestCanvasHandler_HandleCompositions_POST_InvalidJSON(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/api/canvas/compositions", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.HandleCompositions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", w.Code)
	}
}

// TODO(#441): Phase 2-5 - Multi-glyph chain backend tests
func TestCanvasHandler_HandleCompositions_POST_ThreeGlyphs(t *testing.T) {
	t.Skip("TODO(#441): Unskip when 3-glyph chain UI is implemented")

	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	// Create three glyphs (ax, py, prompt)
	glyphs := []*glyphstorage.CanvasGlyph{
		{ID: "ax-1", Symbol: "🜶", X: 100, Y: 100},
		{ID: "py-1", Symbol: "🝓", X: 200, Y: 100},
		{ID: "prompt-1", Symbol: "🝗", X: 300, Y: 100},
	}
	for _, g := range glyphs {
		if err := store.UpsertGlyph(context.Background(), g); err != nil {
			t.Fatalf("UpsertGlyph failed: %v", err)
		}
	}

	// Create 3-glyph composition (ax|py|prompt) - linear chain with 2 edges
	comp := glyphstorage.CanvasComposition{
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

	var response glyphstorage.CanvasComposition
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify 2 edges returned (3-glyph chain = 2 edges)
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

func TestCanvasHandler_HandleCompositions_GET_PreservesGlyphOrder(t *testing.T) {
	t.Skip("TODO(#441): Unskip when 3-glyph chain UI is implemented")

	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	// Create glyphs
	glyphs := []*glyphstorage.CanvasGlyph{
		{ID: "ax-1", Symbol: "🜶", X: 100, Y: 100},
		{ID: "py-1", Symbol: "🝓", X: 200, Y: 100},
		{ID: "prompt-1", Symbol: "🝗", X: 300, Y: 100},
	}
	for _, g := range glyphs {
		if err := store.UpsertGlyph(context.Background(), g); err != nil {
			t.Fatalf("UpsertGlyph failed: %v", err)
		}
	}

	// Create 3-glyph composition directly in storage
	comp := &glyphstorage.CanvasComposition{
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

	var response glyphstorage.CanvasComposition
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Critical: Verify edge order is preserved (left-to-right chain: ax→py→prompt)
	if len(response.Edges) != 2 {
		t.Fatalf("Expected 2 edges (3-glyph chain), got %d", len(response.Edges))
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

func TestCanvasHandler_HandleCompositions_POST_FourGlyphChain(t *testing.T) {
	t.Skip("TODO(#441): Unskip when N-glyph chain extension is implemented")

	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	// Create four glyphs
	glyphs := []*glyphstorage.CanvasGlyph{
		{ID: "ax-1", Symbol: "🜶", X: 100, Y: 100},
		{ID: "py-1", Symbol: "🝓", X: 200, Y: 100},
		{ID: "prompt-1", Symbol: "🝗", X: 300, Y: 100},
		{ID: "prompt-2", Symbol: "🝗", X: 400, Y: 100},
	}
	for _, g := range glyphs {
		if err := store.UpsertGlyph(context.Background(), g); err != nil {
			t.Fatalf("UpsertGlyph failed: %v", err)
		}
	}

	// Create 4-glyph composition (3 edges: ax→py→prompt1→prompt2)
	comp := glyphstorage.CanvasComposition{
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

	var response glyphstorage.CanvasComposition
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify 4-glyph chain stored and retrieved correctly (3 edges)
	if len(response.Edges) != 3 {
		t.Errorf("Expected 3 edges (4-glyph chain), got %d", len(response.Edges))
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

// === Subscription compilation tests ===

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
