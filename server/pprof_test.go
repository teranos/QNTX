package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

// net/http/pprof's init() put these on http.DefaultServeMux. What the node
// serves comes from lines in server/reach, and no line names them.
func TestTheServedHandlerRefusesProfiling(t *testing.T) {
	srv := servedForTest(t)

	paths := []string{
		"/debug/pprof/",
		"/debug/pprof/cmdline",
		"/debug/pprof/heap",
		"/debug/pprof/profile",
		"/debug/pprof/goroutine",
	}
	for _, path := range paths {
		w := httptest.NewRecorder()
		srv.served.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s answered %d on what the node serves", path, w.Code)
		}
	}
}

// Zero means zero. net.Listen reads port 0 as "pick one", so asking for no
// profiling would have got it on an address nobody named. servePprof blocks
// serving a listener it opens, so returning is the observable difference.
func TestPprofPortZeroListensNowhere(t *testing.T) {
	s := &QNTXServer{logger: zap.NewNop().Sugar()}

	returned := make(chan struct{})
	go func() {
		s.servePprof(0)
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("pprof_port 0 opened a listener and blocked serving it")
	}
}

// cmdline is the cheap one to prove: it returns immediately and takes no
// profile, so the private mux can be exercised without burning CPU.
func TestPrivateMuxServesProfiling(t *testing.T) {
	w := httptest.NewRecorder()
	pprofMux().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/debug/pprof/cmdline", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("cmdline answered %d on the private mux", w.Code)
	}
	if !strings.Contains(w.Body.String(), ".test") {
		t.Fatalf("cmdline did not name the running binary: %q", w.Body.String())
	}
}
