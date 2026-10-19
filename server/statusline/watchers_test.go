package statusline

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
	return r.WithContext(auth.WithAdmission(r.Context(), auth.Admitted(auth.LevelRoot)))
}

// "To me it’s important enough to show those kinds of things and their
// details at a higher priority than showing plugin versions"
func TestFailingWatchersLeadTheRow(t *testing.T) {
	h := newWatcherRow(func() storage.Watchers {
		return &fakeWatcherStore{fires: []storage.WatcherErrorFire{
			{WatcherID: "crier-ingest-1", Name: "ingest", AtMs: time.Now().Add(-2 * time.Minute).UnixMilli(), Error: "gave up after 5 attempts"},
		}}
	})

	req := rootContext(httptest.NewRequest(http.MethodGet, "/am/statusline?format=json", nil))
	rec := httptest.NewRecorder()
	h.HandleStatusLine(rec, req)

	var body StatusLineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("row is not json: %v", err)
	}
	// Index 0 is who is looking, pinned there and never rotating; the node's
	// own frame follows, and the failure comes before anything a plugin says.
	if len(body.Items) < 3 {
		t.Fatalf("a failing watcher drew nothing: %+v", body.Items)
	}
	first := body.Items[2]
	if first.Name != "ingest" || first.Symbol != SymbolUnwell {
		t.Fatalf("the failure does not follow the node's frame: %+v", body.Items)
	}
	if first.Note != "2m" {
		t.Fatalf("the row does not say how long ago: %q", first.Note)
	}
}

// The row names it; the click has the whole of it.
func TestFailingWatcherDetailAnswersInFull(t *testing.T) {
	failedAt := time.Now().Add(-30 * time.Second)
	h := newWatcherRow(func() storage.Watchers {
		return &fakeWatcherStore{fires: []storage.WatcherErrorFire{
			{WatcherID: "crier-ingest-1", Name: "ingest", AtMs: failedAt.UnixMilli(), Error: "plugin not loaded"},
		}}
	})

	detail := itemDetail(t, h, rootContext(httptest.NewRequest(http.MethodGet, "/am/statusline/ingest", nil)), "ingest")
	if detail["error"] != "plugin not loaded" {
		t.Fatalf("detail does not carry the error in full: %+v", detail)
	}
	if detail["healthy"] != false {
		t.Fatalf("a failure reads healthy: %+v", detail)
	}
}

// No store wired, no failure items — and no panic reaching for one.
func TestNoWatcherStoreDrawsNoFailures(t *testing.T) {
	h := newWatcherRow(func() storage.Watchers { return nil })

	req := rootContext(httptest.NewRequest(http.MethodGet, "/am/statusline?format=json", nil))
	rec := httptest.NewRecorder()
	h.HandleStatusLine(rec, req)

	var body StatusLineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("row is not json: %v", err)
	}
	// The pinned caller still draws. Nothing unwell does, which is the claim.
	for _, it := range body.Items {
		if it.Symbol == SymbolUnwell {
			t.Fatalf("a failure was drawn with no store: %+v", body.Items)
		}
	}
}

// newWatcherRow is a row over the given watcher store.
func newWatcherRow(src func() storage.Watchers) *StatusLineHandler {
	h := aRow(quietNode{})
	h.watchers = src
	return h
}
