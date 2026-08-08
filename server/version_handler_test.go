package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/teranos/QNTX/internal/version"
)

// The full commit is the point. The WebSocket connect frame already sends
// Short(), so an endpoint that truncated too would answer nothing new.
func TestHandleVersion_ReturnsFullCommit(t *testing.T) {
	store, db := createTestStore(t)
	srv, err := NewQNTXServer(db, store, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	original := version.CommitHash
	version.CommitHash = "019988dd200715175b97a2fcdde47f0e33ccf405"
	defer func() { version.CommitHash = original }()

	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	w := httptest.NewRecorder()

	srv.HandleVersion(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Status = %d, want %d", w.Code, http.StatusOK)
	}

	var got version.Info
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("Body is not version.Info: %v (body %q)", err, w.Body.String())
	}

	if got.CommitHash != version.CommitHash {
		t.Errorf("commit_hash = %q, want the full %q", got.CommitHash, version.CommitHash)
	}
	if got.Platform == "" || got.GoVersion == "" {
		t.Errorf("platform and go_version must be populated, got %+v", got)
	}
}

func TestHandleVersion_RejectsNonGetRequests(t *testing.T) {
	store, db := createTestStore(t)
	srv, err := NewQNTXServer(db, store, ":memory:", 0)
	if err != nil {
		t.Fatalf("Failed to create server: %v", err)
	}

	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			req := httptest.NewRequest(method, "/api/version", nil)
			w := httptest.NewRecorder()

			srv.HandleVersion(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("Status = %d, want %d", w.Code, http.StatusMethodNotAllowed)
			}
		})
	}
}
