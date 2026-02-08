package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	glyphstorage "github.com/teranos/QNTX/glyph/storage"
	pb "github.com/teranos/QNTX/glyph/proto"
	qntxtest "github.com/teranos/QNTX/internal/testing"
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
