package server

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/getsentry/sentry-go"
	"github.com/teranos/QNTX/internal/logger"
	"github.com/teranos/QNTX/internal/measure"
	"github.com/teranos/QNTX/server/a2a"
	"golang.org/x/time/rate"
)

// Sentry watches refused A2A requests; QNTX and Ground keep the node's history.
// A refusal changes nothing the node holds, so it is no attestation and no line in the node's own log.

// refusalKind is what a refusal was: a client asking what the node does not answer, or the node failing.
type refusalKind string

const (
	refusedExpected refusalKind = "expected"
	refusedDefect   refusalKind = "defect"
)

// Logs and issues written per kind; past these a refusal is only counted.
var refusalsWritten = map[refusalKind]*rate.Limiter{
	refusedExpected: rate.NewLimiter(rate.Every(time.Second), 20),
	refusedDefect:   rate.NewLimiter(rate.Every(6*time.Second), 5),
}

// The longest a client's own words are written.
const (
	saidPathMost      = 200
	saidUserAgentMost = 200
	saidVersionMost   = 16
	saidDefectMost    = 500
)

// kindOf is a defect for a 5xx or an answer the node could not give, and expected otherwise.
func kindOf(refused *a2a.Error, status int) refusalKind {
	if status >= http.StatusInternalServerError || refused.Name == "InvalidAgentResponseError" {
		return refusedDefect
	}
	return refusedExpected
}

// refusalSaidKey holds whether a refusal of the request was said to Sentry, so the access log says it no more.
type refusalSaidKey struct{}

func withRefusalSaid(ctx context.Context) (context.Context, *atomic.Bool) {
	said := &atomic.Bool{}
	return context.WithValue(ctx, refusalSaidKey{}, said), said
}

// a2aRefused counts every refused A2A request, and writes down as many as the limits let through.
func (s *QNTXServer) a2aRefused(r *http.Request, refusal a2a.Refusal) {
	if said, held := r.Context().Value(refusalSaidKey{}).(*atomic.Bool); held {
		said.Store(true)
	}
	status, reason := refusal.Err.Said()
	kind := kindOf(refusal.Err, status)
	binding := "http+json"
	if refusal.JSONRPC != "" {
		binding = "jsonrpc"
	}
	version := r.Header.Get("A2A-Version")
	if version == "" {
		version = r.URL.Query().Get("A2A-Version")
	}
	measure.Count(measure.A2ARefused, 1,
		measure.String(measure.AttrOutcome, reason),
		measure.String(measure.AttrKind, string(kind)),
		measure.String(measure.AttrMethod, methodOf(r.Method)),
		measure.String(measure.AttrBinding, binding),
		measure.String(measure.AttrVersion, versionOf(version)))
	if !refusalsWritten[kind].Allow() {
		measure.Count(measure.A2ARefusedUnwritten, 1, measure.String(measure.AttrKind, string(kind)))
		return
	}

	said := map[string]string{
		"a2a.method":    bounded(r.Method, 16),
		"a2a.path":      bounded(a2aPrefix[:len(a2aPrefix)-1]+r.URL.Path, saidPathMost),
		"a2a.operation": refusal.Operation,
		"a2a.binding":   binding,
		"a2a.version":   bounded(version, saidVersionMost),
		"a2a.jsonrpc":   refusal.JSONRPC,
		"a2a.reason":    reason,
		"a2a.kind":      string(kind),
		"a2a.caller":    spokenBy(r),
		"request_id":    logger.RequestIDFrom(r.Context()),
		// What the client says it is; the node does not know that it is.
		"user_agent.untrusted": bounded(r.UserAgent(), saidUserAgentMost),
	}
	if kind == refusedDefect {
		s.a2aDefect(r.Context(), refusal, status, said)
		return
	}
	entry := sentry.NewLogger(r.Context()).Warn()
	for key, value := range said {
		if value != "" {
			entry = entry.String(key, value)
		}
	}
	entry.Int("a2a.status", status).Emit("An A2A request was refused")
}

// a2aDefect is a Sentry issue per operation and reason, and a line in the node's own log.
// The words it was answered with are the node's own, so they are said.
func (s *QNTXServer) a2aDefect(ctx context.Context, refusal a2a.Refusal, status int, said map[string]string) {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}
	hub = hub.Clone()
	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(sentry.LevelError)
		scope.SetFingerprint([]string{"a2a-defect", refusal.Operation, said["a2a.reason"]})
		for key, value := range said {
			if value != "" {
				scope.SetTag(key, value)
			}
		}
		scope.SetContext("a2a", sentry.Context{"status": status, "says": bounded(refusal.Err.Message, saidDefectMost)})
		hub.CaptureMessage("An A2A request failed on the node's side")
	})
	s.logger.Warnw("An A2A request failed on the node's side",
		"operation", refusal.Operation, "reason", said["a2a.reason"], "status", status,
		"request_id", said["request_id"], "says", bounded(refusal.Err.Message, saidDefectMost))
}

// methodOf is the HTTP verb, or OTHER: a count's slices are the node's words, not the client's.
func methodOf(method string) string {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead, http.MethodOptions:
		return method
	}
	return "OTHER"
}

// versionOf is the A2A version a client spoke in one of a few words; empty is 0.3 (§3.6.2).
func versionOf(said string) string {
	switch {
	case said == "":
		return "0.3"
	case said == a2a.Version || strings.HasPrefix(said, a2a.Version+"."):
		return a2a.Version
	case said == "0.3" || strings.HasPrefix(said, "0.3."):
		return "0.3"
	}
	return "other"
}

// bounded is said cut to most runes, its control characters and invalid UTF-8 replaced.
func bounded(said string, most int) string {
	var b strings.Builder
	n := 0
	for len(said) > 0 {
		if n == most {
			b.WriteString("…")
			break
		}
		c, size := utf8.DecodeRuneInString(said)
		said = said[size:]
		if c == utf8.RuneError || unicode.IsControl(c) {
			c = '?'
		}
		b.WriteRune(c)
		n++
	}
	return b.String()
}
