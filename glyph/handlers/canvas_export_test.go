package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	glyphstorage "github.com/teranos/QNTX/glyph/storage"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// TestCanvasExport verifies the core flow: given glyphs, export produces HTML
func TestCanvasExport(t *testing.T) {
	os.Setenv("QNTX_DEMO", "1")
	defer os.Unsetenv("QNTX_DEMO")

	db := qntxtest.CreateTestDB(t)
	store := glyphstorage.NewCanvasStore(db)
	handler := NewCanvasHandler(store)

	// Create a glyph
	width := int32(200)
	height := int32(150)
	err := store.UpsertGlyph(context.Background(), &glyphstorage.CanvasGlyph{
		ID:       "test-glyph",
		CanvasID: "test-canvas",
		Symbol:   "▣",
		X:        100,
		Y:        100,
		Width:    &width,
		Height:   &height,
	})
	require.NoError(t, err)

	// Export the canvas
	req := httptest.NewRequest(http.MethodGet, "/api/canvas/export?canvas_id=test-canvas", nil)
	w := httptest.NewRecorder()
	handler.HandleExportStatic(w, req)

	// Verify: returns 200 and produces HTML
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "<!DOCTYPE html>")
}
