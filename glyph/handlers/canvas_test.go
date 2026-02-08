package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	glyphstorage "github.com/teranos/QNTX/glyph/storage"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

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
		ID:       "comp-1",
		Type:     "ax-prompt",
		GlyphIDs: []string{"glyph-1", "glyph-2"},
		X:        150,
		Y:        150,
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
	if response.Type != comp.Type {
		t.Errorf("Type mismatch: got %s, want %s", response.Type, comp.Type)
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
		{ID: "comp-1", Type: "ax-prompt", GlyphIDs: []string{"g1", "g2"}, X: 100, Y: 100},
		{ID: "comp-2", Type: "ax-py", GlyphIDs: []string{"g2", "g3"}, X: 200, Y: 200},
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
		ID:       "comp-1",
		Type:     "py-prompt",
		GlyphIDs: []string{"glyph-1", "glyph-2"},
		X:        100,
		Y:        200,
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
		ID:       "comp-1",
		Type:     "ax-prompt",
		GlyphIDs: []string{"glyph-1", "glyph-2"},
		X:        100,
		Y:        200,
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
