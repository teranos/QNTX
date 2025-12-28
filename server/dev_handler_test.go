package server

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestHandleDevMode_ReturnsTrue(t *testing.T) {
	// Set up dev mode
	os.Setenv("DEV", "true")
	defer os.Unsetenv("DEV")

	db := createTestDB(t)
	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/dev", nil)
	w := httptest.NewRecorder()

	srv.HandleDevMode(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/plain" {
		t.Errorf("Content-Type = %q, want %q", contentType, "text/plain")
	}

	body := w.Body.String()
	if body != "true" {
		t.Errorf("Body = %q, want %q", body, "true")
	}
}

func TestHandleDevMode_ReturnsFalse(t *testing.T) {
	// Ensure DEV is not set
	os.Unsetenv("DEV")

	db := createTestDB(t)
	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/dev", nil)
	w := httptest.NewRecorder()

	srv.HandleDevMode(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	contentType := w.Header().Get("Content-Type")
	if contentType != "text/plain" {
		t.Errorf("Content-Type = %q, want %q", contentType, "text/plain")
	}

	body := w.Body.String()
	if body != "false" {
		t.Errorf("Body = %q, want %q", body, "false")
	}
}

func TestHandleDevMode_RejectsNonGetRequests(t *testing.T) {
	db := createTestDB(t)
	srv, err := NewQNTXServer(db, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/dev", nil)
			w := httptest.NewRecorder()

			srv.HandleDevMode(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s: Status = %d, want %d", method, w.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}
