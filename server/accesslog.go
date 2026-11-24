package server

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/teranos/QNTX/server/auth"
	errors "github.com/teranos/sacred-error"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
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
	"/am/version":    true,
	"/api/plugins":   true,
	"/am/statusline": true,
}

// polled is whether this request is a poll, by what it asks and what it was
// told. A ground asks for its comet again and again until one is kept, and is
// told 404 each time; the path carries the comet's hash, so no fixed list names
// it. Only that answer is a poll: a 403 or a 429 on the same path is a refusal
// and is said every time, and a POST that builds one is never one.
func polled(r *http.Request, status int) bool {
	if heartbeatPaths[r.URL.Path] {
		return true
	}
	return asksForComet(r) && status == http.StatusNotFound
}

// RoutineSpan is whether an ended span is routine, and so not sent: a poll a
// client asks for on purpose (the version, plugins and statusline, or a ground
// reading its comet), or a plugin update or a forward the worker ran, that
// ended well and within a second. Those were most of the spans the node
// shipped in a week. One that failed, or was slow, is sent: that is when
// someone asks Sentry about it. Only the reading is a poll: a POST to the same
// path, which adds a plugin or builds a comet, is a request.
func RoutineSpan(s sdktrace.ReadOnlySpan) bool {
	if s.Status().Code == codes.Error || s.EndTime().Sub(s.StartTime()) >= routineWithin {
		return false
	}
	switch s.SpanKind() {
	case trace.SpanKindServer:
		var method, path string
		var status int64
		for _, kv := range s.Attributes() {
			switch kv.Key {
			case attrMethod:
				method = kv.Value.AsString()
			case attrPath:
				path = kv.Value.AsString()
			case attrStatus:
				status = kv.Value.AsInt64()
			}
		}
		if status >= http.StatusInternalServerError || (method != http.MethodGet && method != http.MethodHead) {
			return false
		}
		return heartbeatPaths[path] || strings.HasPrefix(path, "/comet/")
	case trace.SpanKindConsumer:
		return routineJobs[s.Name()]
	}
	return false
}

// The attributes otelhttp names a server span's request and answer by.
const (
	attrMethod attribute.Key = "http.request.method"
	attrPath   attribute.Key = "url.path"
	attrStatus attribute.Key = "http.response.status_code"
)

// routineWithin is how long a routine span may take and still be routine.
const routineWithin = time.Second

// routineJobs are the jobs the worker runs most, by their span's name.
var routineJobs = map[string]bool{"process plugin.update": true, "process github.forward": true}

// asksForComet is a GET or HEAD under /comet/: a ground reading, not a build.
func asksForComet(r *http.Request) bool {
	return strings.HasPrefix(r.URL.Path, "/comet/") && (r.Method == http.MethodGet || r.Method == http.MethodHead)
}

// Whether this poll went well: it answered. How long it took is not the
// state; a quick endpoint crossing a latency line and back was a turn each way.

// Any status short of 400, not 200 alone: /am/statusline answers 303 every time,
// so pinning this to 200 quieted nothing and buried every other line.
func heartbeatWell(status int) bool {
	return status < http.StatusBadRequest
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
	held int
}

// How many paths are remembered. A comet's path carries whatever a caller
// writes, so the set is not the three fixed polls and cannot be left to grow.
// Past it a new path is said every time, as every path was before polls were
// remembered.
const heartbeatHolds = 1024

// How long a remembered path can be. The count above bounds the entries and
// this bounds each one, so the set cannot hold more than a few hundred
// kilobytes whatever a caller writes. A comet's path is a few dozen bytes.
const heartbeatPathMax = 256

// worthSaying reports whether this poll is a change worth a line.
// forget drops what was said about a path, so the next answer is news again.
// A comet answered otherwise than 404 has changed, and a 404 after that is said.
func (h *heartbeats) forget(path string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, seen := h.seen.LoadAndDelete(path); seen {
		h.held--
	}
}

func (h *heartbeats) worthSaying(path string, well bool) bool {
	// Read and write as one: two polls landing together would each see the
	// same count and each decide it was the one to speak.
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(path) > heartbeatPathMax {
		return true
	}
	state := heartbeatState{well: well}
	last, seen := h.seen.Load(path)
	if !seen && h.held >= heartbeatHolds {
		return true
	}
	if seen {
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
	if !seen {
		h.held++
	}
	// A path with nothing escaped in it is a slice of the request line, and held
	// as a key it keeps the whole line, query and all, alive. Stored again, an
	// equal key replaces the one held, so every store is of a copy.
	h.seen.Store(strings.Clone(path), state)
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
		ctx, said := withRefusalSaid(ctx)
		next(recorder, r.WithContext(withStarted(ctx, start)))

		took := time.Since(start)
		// Counted before the heartbeat check, or the row would report only the
		// refusals that were also worth a log line.
		s.answers.note(recorder.status)
		if asksForComet(r) && recorder.status != http.StatusNotFound {
			s.heartbeats.forget(r.URL.Path)
		}

		// A refusal Sentry is told of is said there, bounded, and not again here.
		if said.Load() {
			return
		}

		// A polled path says something when its answer turns, and nothing while
		// it stays the same.
		if polled(r, recorder.status) {
			if !s.heartbeats.worthSaying(r.URL.Path, heartbeatWell(recorder.status)) {
				return
			}
		} else if recorder.status < http.StatusBadRequest {
			// Every other path says only what went wrong. A request answered
			// well is the node working, which the count above already holds; as
			// a line it was most of what the node wrote and none of what anyone
			// read.
			return
		}

		// The method, path and status are the message, so identical requests
		// group into one line with a count in the log view rather than a wall of
		// "http". The same three stay as fields below, for filtering.
		//
		// The level and not the identity. Who was admitted is attested into the
		// system namespace (ADR-030), where it is one record of one admission —
		// not a provider account id on every request the node ever answers.
		s.logger.Infow(fmt.Sprintf("http %s %s %d", r.Method, r.URL.Path, recorder.status),
			"method", r.Method,
			"path", r.URL.Path,
			"status", recorder.status,
			"bytes", recorder.bytes,
			"took_ms", took.Milliseconds(),
			"ip", clientIP(r),
			"level", seen.LevelName(),
			"user_agent", r.UserAgent(),
		)
	}
}
