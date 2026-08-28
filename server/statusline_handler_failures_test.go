package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/storage"
)

// logWith builds a failure log holding the given failures.
func logWith(failures ...HandlerFailure) *handlerFailureLog {
	l := newHandlerFailureLog()
	for _, f := range failures {
		l.record(f)
	}
	return l
}

func handlerRow(l *handlerFailureLog) *StatusLineHandler {
	return NewStatusLineHandler(nil, nil, nil,
		func() storage.Watchers { return nil },
		func() *handlerFailureLog { return l }, nil)
}

// A handler failure no longer draws its own item on the row: it is shown inside
// the slot of the plugin that declared it, so the row does not carry the same
// plugin twice under two names.
func TestAHandlerFailureDrawsNoItemOfItsOwn(t *testing.T) {
	h := handlerRow(logWith(HandlerFailure{
		Handler: "capy/capy.campaigns",
		Error:   "rpc error: code = Unavailable",
		AtMs:    time.Now().Add(-2 * time.Minute).UnixMilli(),
	}))

	req := rootContext(httptest.NewRequest(http.MethodGet, "/statusline?format=json", nil))
	rec := httptest.NewRecorder()
	h.HandleStatusLine(rec, req)

	var body StatusLineResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("row is not json: %v", err)
	}
	for _, item := range body.Items {
		if item.Name == "capy/capy.campaigns" {
			t.Fatalf("the failure drew a standalone item: %+v", body.Items)
		}
	}
}

// One item per handler, carrying how many times. Forty failures are one thing
// to fix, and the count is what says it keeps happening.
func TestRepeatedHandlerFailuresCollapseWithACount(t *testing.T) {
	now := time.Now()
	h := handlerRow(logWith(
		HandlerFailure{Handler: "capy.account", Error: "expired", AtMs: now.Add(-3 * time.Hour).UnixMilli()},
		HandlerFailure{Handler: "capy.account", Error: "expired", AtMs: now.Add(-2 * time.Hour).UnixMilli()},
		HandlerFailure{Handler: "capy.account", Error: "expired", AtMs: now.Add(-1 * time.Hour).UnixMilli()},
	))

	items := handlerFailureItemsFor(h.recentHandlerFailures())
	if len(items) != 1 {
		t.Fatalf("three failures of one handler drew %d items: %+v", len(items), items)
	}
	if items[0].Note != "3x expired" {
		t.Fatalf("the row does not carry the count and the reason: %q", items[0].Note)
	}
}

// The row names it; the click has the exact error.
func TestFailingHandlerDetailCarriesTheExactError(t *testing.T) {
	failedAt := time.Now().Add(-30 * time.Second)
	h := handlerRow(logWith(HandlerFailure{
		Handler:        "capy.campaigns",
		ScheduledJobID: "AS-CAPYCAPY-SCHEDULE-PULSE-FZ36C6PW",
		ExecutionID:    "PX-EXECUTIO-ID-PULSE-PJSGBAAE",
		Error:          `rpc error: code = Unavailable desc = closing transport due to: EOF`,
		AtMs:           failedAt.UnixMilli(),
	}))

	req := rootContext(httptest.NewRequest(http.MethodGet, "/statusline/capy.campaigns", nil))
	rec := httptest.NewRecorder()
	h.HandleStatusLineItem(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("detail answered %d", rec.Code)
	}
	var detail map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatalf("detail is not json: %v", err)
	}
	if detail["error"] != `rpc error: code = Unavailable desc = closing transport due to: EOF` {
		t.Fatalf("detail does not carry the error in full: %+v", detail)
	}
	if detail["healthy"] != false {
		t.Fatalf("a failure reads healthy: %+v", detail)
	}
	if detail["scheduled_job_id"] != "AS-CAPYCAPY-SCHEDULE-PULSE-FZ36C6PW" {
		t.Fatalf("detail does not say which job: %+v", detail)
	}
}

// Past the window the row is quiet again, so a handler fixed yesterday is not
// still being reported today.
func TestHandlerFailuresOutsideTheWindowDrawNothing(t *testing.T) {
	h := handlerRow(logWith(HandlerFailure{
		Handler: "capy.account",
		Error:   "long ago",
		AtMs:    time.Now().Add(-handlerFailureWindow - time.Hour).UnixMilli(),
	}))

	if items := handlerFailureItemsFor(h.recentHandlerFailures()); len(items) != 0 {
		t.Fatalf("a failure past the window still draws: %+v", items)
	}
}

// No log wired, no failure items — and no panic reaching for one.
func TestNoHandlerFailureLogDrawsNoFailures(t *testing.T) {
	h := NewStatusLineHandler(nil, nil, nil,
		func() storage.Watchers { return nil }, nil, nil)

	if items := handlerFailureItemsFor(h.recentHandlerFailures()); len(items) != 0 {
		t.Fatalf("no log drew failures: %+v", items)
	}
}

// A job with no handler name must still name something the row can draw and a
// click can find, rather than drawing an empty span.
func TestUnnamedHandlerFallsBackToTheJobID(t *testing.T) {
	s := &QNTXServer{handlerFailures: newHandlerFailureLog()}
	s.noteHandlerFailure(HandlerFailure{
		ScheduledJobID: "AS-CAPYCAPY-SCHEDULE-PULSE-FZ36C6PW",
		Error:          "no name on the job",
	})

	items := handlerFailureItemsFor(groupHandlerFailures(
		s.handlerFailures.since(handlerFailureWindow), handlerFailureItems))
	if len(items) != 1 {
		t.Fatalf("drew %d items: %+v", len(items), items)
	}
	if items[0].Name != "AS-CAPYCAPY-SCHEDULE-PULSE-FZ36C6PW" {
		t.Fatalf("the row drew a nameless span: %q", items[0].Name)
	}
}

// The log is bounded, so a handler failing in a tight loop cannot grow it.
func TestHandlerFailureLogIsBounded(t *testing.T) {
	l := newHandlerFailureLog()
	now := time.Now().UnixMilli()
	for i := 0; i < handlerFailureLogSize*2; i++ {
		l.record(HandlerFailure{Handler: "capy.script", Error: "no", AtMs: now})
	}
	if got := len(l.since(handlerFailureWindow)); got != handlerFailureLogSize {
		t.Fatalf("log holds %d, want %d", got, handlerFailureLogSize)
	}
}
