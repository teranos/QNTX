package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// "only the thing that is also shown to a user"
// 「[Opus 5.5] A request that failed on the node's side is what the person faced, and」
// 「[Opus 5.5] said as faced; a refusal is an answer, and a 503 a state, and neither is.」
func TestWhatAPersonWasAnsweredWithIsFacedOnlyWhenTheNodeFailed(t *testing.T) {
	for _, c := range []struct {
		status int
		faced  bool
	}{
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
		{http.StatusServiceUnavailable, false},
		{http.StatusNotFound, false},
		{http.StatusConflict, false},
	} {
		core, said := observer.New(zap.DebugLevel)
		writeErrorEnvelope(httptest.NewRecorder(), zap.New(core).Sugar(), "", errors.New("the store is gone"), c.status)
		entries := said.All()
		if len(entries) != 1 {
			t.Fatalf("%d: said %d lines, want 1", c.status, len(entries))
		}
		_, marked := entries[0].ContextMap()["faced"]
		if marked != c.faced || (entries[0].Level == zapcore.ErrorLevel) != c.faced {
			t.Errorf("%d: said at %s, faced %v; want faced %v", c.status, entries[0].Level, marked, c.faced)
		}
	}
}
