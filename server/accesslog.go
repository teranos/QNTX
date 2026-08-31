package server

import (
	"bufio"
	"net"
	"net/http"
	"sync"
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

// Paths a client is expected to poll hard and continuously. The version is
// asked for on purpose and often, and that is not a fault to be recorded.
var heartbeatPaths = map[string]bool{
	"/api/version": true,
	"/api/plugins": true,
	"/statusline":  true,
}

// Past this a heartbeat has stopped being one, and is worth a line.
const heartbeatQuiet = 50 * time.Millisecond

// Whether this answer carries nothing. A heartbeat that refuses, fails or drags
// still says something; the hundredth identical fast answer does not.

// Any status short of 400, not 200 alone: /statusline answers 303 every time,
// so pinning this to 200 quieted nothing and buried every other line under a
// poll that runs once a second.
func heartbeat(path string, status int, took time.Duration) bool {
	return heartbeatPaths[path] && status < http.StatusBadRequest && took < heartbeatQuiet
}

// How often a dull heartbeat is still worth saying out loud.
const heartbeatEvery = time.Minute

// A poll nobody can see is a poll nobody can tell has stopped. So a heartbeat
// is not silenced, it is thinned: the first one speaks, and then one a minute.
type heartbeats struct {
	said sync.Map // path -> time.Time of the last line
}

// worthSaying reports whether this heartbeat is the one that gets a line.
func (h *heartbeats) worthSaying(path string, now time.Time) bool {
	last, seen := h.said.Load(path)
	if seen {
		if when, ok := last.(time.Time); ok && now.Sub(when) < heartbeatEvery {
			return false
		}
	}
	h.said.Store(path, now)
	return true
}

// accessLog records every request that reaches the node: who, what, the answer
// and how long it took. It runs outermost, so a refusal by any middleware below
// is recorded too — a 429 and a 403 are the two things worth seeing first.
func (s *QNTXServer) accessLog(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		// 200 is what net/http writes when a handler never calls WriteHeader.
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		// Middleware passes the admission down on a copy of the request, so the
		// one this layer holds never learns it. The sink is where it is written.
		ctx, seen := auth.WithAdmissionSink(r.Context())
		next(recorder, r.WithContext(ctx))

		took := time.Since(start)
		// Counted before the heartbeat check, or the row would report only the
		// refusals that were also worth a log line.
		s.answers.note(recorder.status)

		if heartbeat(r.URL.Path, recorder.status, took) && !s.heartbeats.worthSaying(r.URL.Path, time.Now()) {
			return
		}

		// FIXME: one info line per request, to every sink. A request is a count
		// and a duration; this says the same thing thousands of times and
		// buries the lines that mean something.
		s.logger.Infow("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"bytes", recorder.bytes,
			"took_ms", took.Milliseconds(),
			"ip", clientIP(r),
			"identity", seen.Identity,
			"level", string(seen.Level),
			"user_agent", r.UserAgent(),
		)
	}
}
