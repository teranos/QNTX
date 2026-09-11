package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/teranos/QNTX/ats/watcher"
	"go.uber.org/zap"
)

// A watcher that is running and cannot be seen is a black box, and a standing
// one is held in no store — so a list that reads only the store shows a node
// watching less than it watches. These pin the two routes that answer about
// watchers against the table itself.

// watcherHandlerUnderTest is a handler over an engine holding an empty store,
// so what comes back is the standing table and nothing else.
func watcherHandlerUnderTest(t *testing.T) *WatcherHandler {
	t.Helper()
	_, db := createTestStore(t)
	t.Cleanup(func() { _ = db.Close() })

	engine := watcher.NewEngine(db, nil, "http://127.0.0.1:0", zap.NewNop().Sugar())
	return NewWatcherHandler(engine, zap.NewNop().Sugar(), nil)
}

func listedWatchers(t *testing.T, h *WatcherHandler) []WatcherResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	h.handleListWatchers(rec, httptest.NewRequest(http.MethodGet, "/api/watchers", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("the watcher list answered %d: %s", rec.Code, rec.Body.String())
	}
	var listed []WatcherResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("the watcher list did not answer with watchers: %v", err)
	}
	return listed
}

func TestTheWatcherListShowsWhatTheNodeIsBornWatching(t *testing.T) {
	h := watcherHandlerUnderTest(t)

	listed := listedWatchers(t, h)
	for _, standing := range watcher.Standing() {
		found := false
		for _, row := range listed {
			if row.ID != standing.ID {
				continue
			}
			found = true
			if !row.Standing {
				t.Errorf("%s is listed without saying it is standing, so it reads as one "+
					"somebody made and could delete", row.ID)
			}
		}
		if !found {
			t.Errorf("%s is watching and the list does not show it", standing.ID)
		}
	}
}

func TestOneStandingWatcherCanBeAskedAboutByID(t *testing.T) {
	h := watcherHandlerUnderTest(t)

	for _, standing := range watcher.Standing() {
		rec := httptest.NewRecorder()
		h.handleGetWatcher(rec, httptest.NewRequest(http.MethodGet, "/api/watchers/"+standing.ID, nil), standing.ID)

		if rec.Code != http.StatusOK {
			t.Errorf("asking about %s answered %d; it is running", standing.ID, rec.Code)
			continue
		}
		var answer WatcherResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &answer); err != nil {
			t.Errorf("asking about %s did not answer with a watcher: %v", standing.ID, err)
			continue
		}
		if !answer.Standing {
			t.Errorf("%s answered without saying it is standing", standing.ID)
		}
	}
}

// A standing watcher is held in no store, so a write would save a row the
// engine ignores — saved, doing nothing, and looking like the thing running.
func TestAStandingWatcherCannotBeWrittenThroughTheRoutes(t *testing.T) {
	h := watcherHandlerUnderTest(t)

	for _, standing := range watcher.Standing() {
		for what, call := range map[string]func(*httptest.ResponseRecorder){
			"update": func(rec *httptest.ResponseRecorder) {
				h.handleUpdateWatcher(rec, httptest.NewRequest(http.MethodPut, "/api/watchers/"+standing.ID, nil), standing.ID)
			},
			"delete": func(rec *httptest.ResponseRecorder) {
				h.handleDeleteWatcher(rec, httptest.NewRequest(http.MethodDelete, "/api/watchers/"+standing.ID, nil), standing.ID)
			},
		} {
			rec := httptest.NewRecorder()
			call(rec)
			if rec.Code != http.StatusConflict {
				t.Errorf("%s on %s answered %d, want %d", what, standing.ID, rec.Code, http.StatusConflict)
			}
		}
	}
}
