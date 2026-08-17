package server

import (
	"fmt"
	"net/http"
	netpprof "net/http/pprof"
	"strings"
	"time"

	"github.com/teranos/errors"
)

// pprofPrefix is what net/http/pprof's init() registers on
// http.DefaultServeMux whenever the package is linked in, blank import or not.
const pprofPrefix = "/debug/pprof/"

// withoutPprof answers 404 for the profiling endpoints. They reached the
// served mux through no auth wrapper and no rate limiter.
func withoutPprof(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, pprofPrefix) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// pprofMux serves the profiling endpoints by naming their handlers, rather
// than by inheriting whatever else is on the default mux.
func pprofMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc(pprofPrefix, netpprof.Index)
	mux.HandleFunc(pprofPrefix+"cmdline", netpprof.Cmdline)
	mux.HandleFunc(pprofPrefix+"profile", netpprof.Profile)
	mux.HandleFunc(pprofPrefix+"symbol", netpprof.Symbol)
	mux.HandleFunc(pprofPrefix+"trace", netpprof.Trace)
	return mux
}

// servePprof runs the profiling endpoints on their own loopback listener. A
// reverse proxy makes every public request arrive from 127.0.0.1, so a
// separate port is what tells a stranger from the box itself.
func (s *QNTXServer) servePprof(port int) {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           pprofMux(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.logger.Infow("Profiling endpoints listening", "addr", addr, "path", pprofPrefix)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.logger.Errorw("Profiling listener stopped; /debug/pprof is unreachable",
			"addr", addr, "error", err)
	}
}
