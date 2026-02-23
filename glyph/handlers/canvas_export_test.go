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

	req := httptest.NewRequest(http.MethodGet, "/api/canvas/export/static", nil)
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

	// Create glyphs of various types
	pyContent := "print('hello')"
	noteContent := "# My Note"
	glyphs := []*glyphstorage.CanvasGlyph{
		{ID: "py-1", Symbol: "py", X: 100, Y: 200, Content: &pyContent},
		{ID: "ax-1", Symbol: sym.AX, X: 400, Y: 200},
		{ID: "note-1", Symbol: sym.Prose, X: 100, Y: 500, Content: &noteContent},
	}

	ctx := t.Context()
	for _, g := range glyphs {
		if err := store.UpsertGlyph(ctx, g); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/canvas/export/static", nil)
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
