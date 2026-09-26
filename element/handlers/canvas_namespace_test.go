package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	elementstorage "github.com/teranos/QNTX/element/storage"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/errors"
)

// "system namespace should have no canvas"
func TestANamespaceWithNoCanvasAnswers404(t *testing.T) {
	handler := NewCanvasHandler(nil, WithCanvasFor(func(*http.Request) (*elementstorage.CanvasStore, error) {
		return nil, errors.Wrap(elementstorage.ErrNoCanvas, "system")
	}))

	for _, call := range []struct {
		name   string
		handle http.HandlerFunc
		path   string
	}{
		{"canvas", handler.HandleCanvas, "/api/canvas"},
		{"elements", handler.HandleElements, "/api/canvas/elements"},
		{"compositions", handler.HandleCompositions, "/api/canvas/compositions"},
		{"minimized windows", handler.HandleMinimizedWindows, "/api/canvas/minimized-windows"},
	} {
		w := httptest.NewRecorder()
		call.handle(w, httptest.NewRequest(http.MethodGet, call.path, nil))
		if w.Code != http.StatusNotFound {
			t.Errorf("%s: got %d, want 404", call.name, w.Code)
		}
	}
}

// "and for every other namespace the canvas needs to be explicitly created and named."
func TestACanvasIsCreatedAndNamedBeforeItHoldsAnything(t *testing.T) {
	store := elementstorage.NewCanvasStore(qntxtest.CreateTestDB(t))
	handler := NewCanvasHandler(nil, WithCanvasFor(func(*http.Request) (*elementstorage.CanvasStore, error) {
		return store, nil
	}))

	w := httptest.NewRecorder()
	handler.HandleElements(w, httptest.NewRequest(http.MethodGet, "/api/canvas/elements", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("elements before the canvas is created: got %d, want 404", w.Code)
	}

	w = httptest.NewRecorder()
	handler.HandleCanvas(w, httptest.NewRequest(http.MethodPost, "/api/canvas", bytes.NewBufferString(`{"name":"garden"}`)))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: got %d, want 201: %s", w.Code, w.Body)
	}

	w = httptest.NewRecorder()
	handler.HandleCanvas(w, httptest.NewRequest(http.MethodGet, "/api/canvas", nil))
	if w.Code != http.StatusOK || !bytes.Contains(w.Body.Bytes(), []byte(`"garden"`)) {
		t.Fatalf("name: got %d %s, want 200 garden", w.Code, w.Body)
	}

	w = httptest.NewRecorder()
	handler.HandleElements(w, httptest.NewRequest(http.MethodGet, "/api/canvas/elements", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("elements after the canvas is created: got %d, want 200", w.Code)
	}

	w = httptest.NewRecorder()
	handler.HandleCanvas(w, httptest.NewRequest(http.MethodPost, "/api/canvas", bytes.NewBufferString(`{"name":"again"}`)))
	if w.Code != http.StatusConflict {
		t.Fatalf("second create: got %d, want 409", w.Code)
	}
}

// Nothing crosses: two namespaces' canvases are two stores.
func TestTwoNamespacesSeeTheirOwnCanvas(t *testing.T) {
	stores := map[string]*elementstorage.CanvasStore{
		"a": elementstorage.NewCanvasStore(qntxtest.CreateTestDB(t)),
		"b": elementstorage.NewCanvasStore(qntxtest.CreateTestDB(t)),
	}
	for name, store := range stores {
		if err := store.Create(t.Context(), name, "", ""); err != nil {
			t.Fatal(err)
		}
	}
	handler := NewCanvasHandler(nil, WithCanvasFor(func(r *http.Request) (*elementstorage.CanvasStore, error) {
		return stores[r.Header.Get("X-Test-Namespace")], nil
	}))

	post := httptest.NewRequest(http.MethodPost, "/api/canvas/elements", bytes.NewBufferString(`{"id":"only-in-a","symbol":"⋈"}`))
	post.Header.Set("X-Test-Namespace", "a")
	w := httptest.NewRecorder()
	handler.HandleElements(w, post)
	if w.Code != http.StatusOK {
		t.Fatalf("post to a: got %d: %s", w.Code, w.Body)
	}

	get := httptest.NewRequest(http.MethodGet, "/api/canvas/elements/only-in-a", nil)
	get.Header.Set("X-Test-Namespace", "b")
	w = httptest.NewRecorder()
	handler.HandleElements(w, get)
	if w.Code != http.StatusNotFound {
		t.Fatalf("b sees a's element: got %d, want 404", w.Code)
	}
}
