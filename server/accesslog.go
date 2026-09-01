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

// Whether this poll went well: it answered, and answered quickly.

// Any status short of 400, not 200 alone: /statusline answers 303 every time,
// so pinning this to 200 quieted nothing and buried every other line.
func heartbeatWell(status int, took time.Duration) bool {
	return status < http.StatusBadRequest && took < heartbeatQuiet
}

// How many lines one state is worth before it stops being news. Two and not
// one, because a single line is easy to miss and easy to lose to a restart.
const heartbeatSays = 2

// What a path last answered, and how much has been said about it.
type heartbeatState struct {
	well bool
	said int
}

// A state is not worth saying; a change is.

// A poll that succeeds carries nothing — the answer was known before it was
// asked, which is why it is asked. A poll failing for an hour is one fact and
// not three thousand. So each run says itself twice and then goes quiet.
type heartbeats struct {
	seen sync.Map // path -> heartbeatState
	mu   sync.Mutex
}

// worthSaying reports whether this poll is a change worth a line.
func (h *heartbeats) worthSaying(path string, well bool) bool {
	// Read and write as one: two polls landing together would each see the
	// same count and each decide it was the one to speak.
	h.mu.Lock()
	defer h.mu.Unlock()

	state := heartbeatState{well: well}
	if last, seen := h.seen.Load(path); seen {
		// A different answer resets the count, which is what makes a change
		// always said and a state never.
		if was, ok := last.(heartbeatState); ok && was.well == well {
			state.said = was.said
		}
	}
	if state.said >= heartbeatSays {
		return false
	}
	state.said++
	h.seen.Store(path, state)
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

		// A polled path says something when its answer turns, and nothing while
		// it stays the same. Every other path still says every request.
		if heartbeatPaths[r.URL.Path] &&
			!s.heartbeats.worthSaying(r.URL.Path, heartbeatWell(recorder.status, took)) {
			return
		}

		// FIXME: identity is on this line at info, so a provider account id
		// reaches every sink on every request that is not a poll. Quieting the
		// line would hide that rather than fix it; the field belongs off it.
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
