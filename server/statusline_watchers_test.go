package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/server/auth"
)

// A store that answers only the question the row asks. The embedded interface
// carries the rest of the contract unimplemented — nothing here calls it.
type fakeWatcherStore struct {
	storage.Watchers
	fires []storage.WatcherErrorFire
}

func (f *fakeWatcherStore) RecentErrorFires(ctx context.Context, sinceMs int64, limit int) ([]storage.WatcherErrorFire, error) {
	if len(f.fires) > limit {
		return f.fires[:limit], nil
	}
	return f.fires, nil
}

func rootContext(r *http.Request) *http.Request {
	return r.WithContext(auth.WithCaller(r.Context(), auth.Caller{Level: auth.LevelRoot}))
}

// "it's important enough to show those kinds of things and their details at a
// higher priority than showing plugin versions"
func TestFailingWatchersLeadTheRow(t *testing.T) {
	h := NewStatusLineHandler(nil, nil, nil, func() storage.Watchers {
		return &fakeWatcherStore{fires: []storage.WatcherErrorFire{
			{WatcherID: "crier-ingest-1", Name: "ingest", AtMs: time.Now().Add(-2 * time.Minute).UnixMilli(), Error: "gave up after 5 attempts"},
		}}
	})

	req := rootContext(httptest.NewRequest(http.MethodGet, "/statusline?format=json", nil))
	rec := httptest.NewRecorder()
	h.HandleStatusLine(rec, req)

	var body StatusLineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("row is not json: %v", err)
	}
	if len(body.Items) == 0 {
		t.Fatal("a failing watcher drew nothing")
	}
	first := body.Items[0]
	if first.Name != "ingest" || first.Glyph != GlyphUnwell {
		t.Fatalf("the failure does not lead the row: %+v", first)
	}
	if first.Note != "2m" {
		t.Fatalf("the row does not say how long ago: %q", first.Note)
	}
}

// The row names it; the click has the whole of it.
func TestFailingWatcherDetailAnswersInFull(t *testing.T) {
	failedAt := time.Now().Add(-30 * time.Second)
	h := NewStatusLineHandler(nil, nil, nil, func() storage.Watchers {
		return &fakeWatcherStore{fires: []storage.WatcherErrorFire{
			{WatcherID: "crier-ingest-1", Name: "ingest", AtMs: failedAt.UnixMilli(), Error: "plugin not loaded"},
		}}
	})

	req := rootContext(httptest.NewRequest(http.MethodGet, "/statusline/ingest", nil))
	rec := httptest.NewRecorder()
	h.HandleStatusLineItem(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("detail answered %d", rec.Code)
	}
	var detail map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("detail is not json: %v", err)
	}
	if detail["error"] != "plugin not loaded" {
		t.Fatalf("detail does not carry the error in full: %+v", detail)
	}
	if detail["healthy"] != false {
		t.Fatalf("a failure reads healthy: %+v", detail)
	}
}

// No store wired, no failure items — and no panic reaching for one.
func TestNoWatcherStoreDrawsNoFailures(t *testing.T) {
	h := NewStatusLineHandler(nil, nil, nil, func() storage.Watchers { return nil })

	req := rootContext(httptest.NewRequest(http.MethodGet, "/statusline?format=json", nil))
	rec := httptest.NewRecorder()
	h.HandleStatusLine(rec, req)

	var body StatusLineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("row is not json: %v", err)
	}
	if len(body.Items) != 0 {
		t.Fatalf("items drawn with no store: %+v", body.Items)
	}
}
