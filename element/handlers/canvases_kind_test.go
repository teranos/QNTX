package handlers

import (
	"bytes"
	"go.uber.org/zap"
	"net/http"
	"net/http/httptest"
	"testing"

	elementstorage "github.com/teranos/QNTX/element/storage"
	"github.com/teranos/QNTX/internal/admission"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// "nil is nil"
func TestACanvasNamingNoKindIsRefused(t *testing.T) {
	store := elementstorage.NewCanvasStore(qntxtest.CreateTestDB(t))
	handler := NewCanvasHandler(nil, zap.NewNop().Sugar(), WithCanvasFor(func(*http.Request) (*elementstorage.CanvasStore, error) {
		return store, nil
	}))
	create := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/canvases", bytes.NewBufferString(body))
		r = r.WithContext(admission.WithAdmission(r.Context(), admission.Admission{UserID: "US-ADA"}))
		w := httptest.NewRecorder()
		handler.HandleCanvases(w, r)
		return w
	}

	if w := create(`{"name":"sketches"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("a canvas naming no kind: got %d, want 400: %s", w.Code, w.Body)
	}
	if w := create(`{"name":"sketches","kind":"user"}`); w.Code != http.StatusCreated {
		t.Fatalf("a User's canvas: got %d, want 201: %s", w.Code, w.Body)
	}
}
