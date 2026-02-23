package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	glyphstorage "github.com/teranos/QNTX/glyph/storage"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/sym"
)

func TestCanvasHandler_HandleExportStatic_Empty(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/canvas/export/static?canvas_id=subcanvas-1", nil)
	w := httptest.NewRecorder()

	handler.HandleExportStatic(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "<!DOCTYPE html>") {
		t.Fatal("response should be an HTML document")
	}
	if !strings.Contains(body, "canvas-workspace") {
		t.Fatal("response should contain canvas-workspace class")
	}
	if w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("expected text/html content type, got %s", w.Header().Get("Content-Type"))
	}
	if !strings.Contains(w.Header().Get("Content-Disposition"), "canvas.html") {
		t.Fatalf("expected attachment disposition, got %s", w.Header().Get("Content-Disposition"))
	}
}

func TestCanvasHandler_HandleExportStatic_WithGlyphs(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	canvasID := "subcanvas-abc"

	// Create glyphs belonging to the target subcanvas
	pyContent := "print('hello')"
	noteContent := "# My Note"
	glyphs := []*glyphstorage.CanvasGlyph{
		{ID: "py-1", Symbol: "py", X: 100, Y: 200, Content: &pyContent, CanvasID: canvasID},
		{ID: "ax-1", Symbol: sym.AX, X: 400, Y: 200, CanvasID: canvasID},
		{ID: "note-1", Symbol: sym.Prose, X: 100, Y: 500, Content: &noteContent, CanvasID: canvasID},
	}

	ctx := t.Context()
	for _, g := range glyphs {
		if err := store.UpsertGlyph(ctx, g); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/canvas/export/static?canvas_id="+canvasID, nil)
	w := httptest.NewRecorder()

	handler.HandleExportStatic(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()

	// Verify glyphs appear in output
	if !strings.Contains(body, "canvas-py-glyph") {
		t.Fatal("should contain Python glyph class")
	}
	if !strings.Contains(body, "canvas-ax-glyph") {
		t.Fatal("should contain AX glyph class")
	}
	if !strings.Contains(body, "canvas-note-glyph") {
		t.Fatal("should contain Note glyph class")
	}
	if !strings.Contains(body, "print(&#39;hello&#39;)") {
		t.Fatal("should contain escaped Python content")
	}
	if !strings.Contains(body, "# My Note") {
		t.Fatal("should contain note content")
	}
	if !strings.Contains(body, "left:100px") {
		t.Fatal("should contain glyph position")
	}
}

func TestCanvasHandler_HandleExportStatic_MissingCanvasID(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/canvas/export/static", nil)
	w := httptest.NewRecorder()

	handler.HandleExportStatic(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCanvasHandler_HandleExportStatic_CanvasIsolation(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	ctx := t.Context()

	// Glyph on root canvas
	rootContent := "root glyph content"
	if err := store.UpsertGlyph(ctx, &glyphstorage.CanvasGlyph{
		ID: "root-1", Symbol: "py", X: 10, Y: 10, Content: &rootContent, CanvasID: "",
	}); err != nil {
		t.Fatal(err)
	}

	// Glyph on target subcanvas
	subContent := "sub glyph content"
	if err := store.UpsertGlyph(ctx, &glyphstorage.CanvasGlyph{
		ID: "sub-1", Symbol: sym.AX, X: 20, Y: 20, Content: &subContent, CanvasID: "subcanvas-target",
	}); err != nil {
		t.Fatal(err)
	}

	// Glyph on a different subcanvas
	otherContent := "other glyph content"
	if err := store.UpsertGlyph(ctx, &glyphstorage.CanvasGlyph{
		ID: "other-1", Symbol: sym.Prose, X: 30, Y: 30, Content: &otherContent, CanvasID: "subcanvas-other",
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/canvas/export/static?canvas_id=subcanvas-target", nil)
	w := httptest.NewRecorder()

	handler.HandleExportStatic(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	body := w.Body.String()

	// Target canvas glyph should appear
	if !strings.Contains(body, "sub glyph content") {
		t.Fatal("should contain target subcanvas glyph content")
	}

	// Root canvas glyph should NOT appear
	if strings.Contains(body, "root glyph content") {
		t.Fatal("should NOT contain root canvas glyph content")
	}

	// Other subcanvas glyph should NOT appear
	if strings.Contains(body, "other glyph content") {
		t.Fatal("should NOT contain other subcanvas glyph content")
	}
}

func TestCanvasHandler_HandleExportStatic_MethodNotAllowed(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	req := httptest.NewRequest(http.MethodPost, "/api/canvas/export/static", nil)
	w := httptest.NewRecorder()

	handler.HandleExportStatic(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", w.Code)
	}
}
