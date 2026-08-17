package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func loggedServer(t *testing.T) (*QNTXServer, *observer.ObservedLogs) {
	t.Helper()
	core, logs := observer.New(zap.InfoLevel)
	return &QNTXServer{logger: zap.New(core).Sugar()}, logs
}

func TestEveryRequestIsRecorded(t *testing.T) {
	s, logs := loggedServer(t)
	handler := s.accessLog(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	handler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/things", nil))

	entries := logs.FilterMessage("http").All()
	if len(entries) != 1 {
		t.Fatalf("logged %d http lines, want 1", len(entries))
	}

	fields := entries[0].ContextMap()
	if fields["status"] != int64(http.StatusTeapot) {
		t.Errorf("status = %v, want %d", fields["status"], http.StatusTeapot)
	}
	if fields["path"] != "/api/things" {
		t.Errorf("path = %v, want /api/things", fields["path"])
	}
	if fields["method"] != http.MethodGet {
		t.Errorf("method = %v, want GET", fields["method"])
	}
}

// A handler that never calls WriteHeader still answered 200, and a log that
// said 0 would be reporting the recorder rather than the response.
func TestAHandlerThatWritesNothingIsRecordedAs200(t *testing.T) {
	s, logs := loggedServer(t)
	handler := s.accessLog(func(w http.ResponseWriter, r *http.Request) {})

	handler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	fields := logs.FilterMessage("http").All()[0].ContextMap()
	if fields["status"] != int64(http.StatusOK) {
		t.Errorf("status = %v, want 200", fields["status"])
	}
}

// The point of running outermost: a refusal from a middleware below is the
// thing most worth seeing, and it never reaches the handler to record itself.
func TestARefusalIsRecordedToo(t *testing.T) {
	s, logs := loggedServer(t)
	handler := s.accessLog(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
	})

	handler(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/attestations", nil))

	fields := logs.FilterMessage("http").All()[0].ContextMap()
	if fields["status"] != int64(http.StatusTooManyRequests) {
		t.Errorf("status = %v, want 429", fields["status"])
	}
}

// Bytes written are how a 200 that returned nothing is told from one that
// returned everything.
func TestTheSizeOfTheAnswerIsRecorded(t *testing.T) {
	s, logs := loggedServer(t)
	handler := s.accessLog(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("twelve bytes"))
	})

	handler(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	fields := logs.FilterMessage("http").All()[0].ContextMap()
	if fields["bytes"] != int64(12) {
		t.Errorf("bytes = %v, want 12", fields["bytes"])
	}
}
