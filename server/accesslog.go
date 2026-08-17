package server

import (
	"bufio"
	"net"
	"net/http"
	"time"

	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/errors"
)

// statusRecorder remembers what was written, because a 502 nobody recorded is
// a 502 found by hand.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Hijack lets a WebSocket upgrade through. Without this the recorder is not a
// Hijacker and every upgrade fails, which is a loud way to learn about wrapping.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("the underlying ResponseWriter cannot be hijacked")
	}
	return hijacker.Hijack()
}

// Flush passes through for streaming handlers.
func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// accessLog records every request that reaches the node: who, what, the answer
// and how long it took. It runs outermost, so a refusal by any middleware below
// is recorded too — a 429 and a 403 are the two things worth seeing first.
func (s *QNTXServer) accessLog(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// 200 is what net/http writes when a handler never calls WriteHeader.
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next(recorder, r)

		// Identity is only known after auth ran, which is why this is read here
		// and not at the top. Empty means the request never got that far.
		identity := ""
		level := ""
		if caller, ok := auth.CallerFrom(r.Context()); ok {
			identity = caller.Identity
			level = string(caller.Level)
		}

		s.logger.Infow("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"bytes", recorder.bytes,
			"took_ms", time.Since(start).Milliseconds(),
			"ip", clientIP(r),
			"identity", identity,
			"level", level,
			"user_agent", r.UserAgent(),
		)
	}
}
