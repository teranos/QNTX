package server

import (
	"strconv"
	"sync"
	"time"
)

// A handler failing is a fix-now kind of event (SIEVE). Watchers already draw
// their failures on the row; handlers are what actually run, and a plugin can
// report healthy while every one of its handlers fails on every pass.

const (
	// Longer than the watcher window: a handler that fails once an hour is the
	// case worth catching, and an hour-wide window shows it only in the minutes
	// after each failure.
	handlerFailureWindow = 24 * time.Hour
	// The row is one line; the newest failing handlers name the fire.
	handlerFailureItems = 3
	// Bounded: a handler failing in a tight loop must not grow this without end.
	handlerFailureLogSize = 256
)

// HandlerFailure is one handler execution that did not succeed.
type HandlerFailure struct {
	// The ATS code the scheduled job named, e.g. "capy.campaigns".
	Handler string
	// Which run this was, so the failure can be followed into execution history.
	ScheduledJobID string
	ExecutionID    string
	// What the plugin said, verbatim, and whatever it attached.
	Error   string
	Details []string
	// How long the run took before it failed.
	DurationMs int
	AtMs       int64
}

// handlerFailureLog holds recent failures in memory. A restart empties it,
// which is correct: the row reports what this process has seen.
type handlerFailureLog struct {
	mu       sync.Mutex
	failures []HandlerFailure
}

func newHandlerFailureLog() *handlerFailureLog {
	return &handlerFailureLog{}
}

// record appends a failure, dropping the oldest once the log is full.
func (l *handlerFailureLog) record(f HandlerFailure) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures = append(l.failures, f)
	if len(l.failures) > handlerFailureLogSize {
		l.failures = l.failures[len(l.failures)-handlerFailureLogSize:]
	}
}

// since returns the failures inside the window, newest first.
func (l *handlerFailureLog) since(window time.Duration) []HandlerFailure {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	cutoff := time.Now().Add(-window).UnixMilli()
	out := make([]HandlerFailure, 0, len(l.failures))
	for i := len(l.failures) - 1; i >= 0; i-- {
		if l.failures[i].AtMs >= cutoff {
			out = append(out, l.failures[i])
		}
	}
	return out
}

// handlerFailureRun is every failure one handler had inside the window. One row
// item per handler: a handler that failed forty times is one thing to fix.
type handlerFailureRun struct {
	Handler string
	Count   int
	Latest  HandlerFailure
}

// groupHandlerFailures collapses failures per handler, most recently failed
// first, capped at what the row can hold. Input is newest first, so the first
// failure seen for a handler is its latest.
func groupHandlerFailures(failures []HandlerFailure, limit int) []handlerFailureRun {
	runs := make([]handlerFailureRun, 0, len(failures))
	at := make(map[string]int, len(failures))
	for _, f := range failures {
		if i, ok := at[f.Handler]; ok {
			runs[i].Count++
			continue
		}
		at[f.Handler] = len(runs)
		runs = append(runs, handlerFailureRun{Handler: f.Handler, Count: 1, Latest: f})
	}
	if limit > 0 && len(runs) > limit {
		runs = runs[:limit]
	}
	return runs
}

// recentHandlerFailures reads the failing handlers inside the window, or
// nothing when no log is wired.
func (h *StatusLineHandler) recentHandlerFailures() []handlerFailureRun {
	if h == nil || h.handlerFailures == nil {
		return nil
	}
	return groupHandlerFailures(h.handlerFailures().since(handlerFailureWindow), handlerFailureItems)
}

// handlerFailureItemsFor spells failing handlers as row items. A single failure
// says only how long ago; the count earns its characters past one.
func handlerFailureItemsFor(runs []handlerFailureRun) []StatusItem {
	items := make([]StatusItem, 0, len(runs))
	for _, r := range runs {
		note := shortAgo(time.Since(time.UnixMilli(r.Latest.AtMs)))
		if r.Count > 1 {
			note = strconv.Itoa(r.Count) + "x " + note
		}
		items = append(items, StatusItem{Name: r.Handler, Note: note, Glyph: GlyphUnwell})
	}
	return items
}

// handlerFailureDetail answers /statusline/{name} for a failing handler: the
// exact error, in full. Matches the truncated-name rule the rest of the row
// uses, since tmux click ranges shorten what they carry.
func (h *StatusLineHandler) handlerFailureDetail(name string) (map[string]any, bool) {
	for _, r := range h.recentHandlerFailures() {
		if r.Handler != name && rangeName(r.Handler) != name {
			continue
		}
		detail := map[string]any{
			"name":             r.Handler,
			"handler":          r.Handler,
			"healthy":          false,
			"failures":         r.Count,
			"window":           handlerFailureWindow.String(),
			"failed_at":        time.UnixMilli(r.Latest.AtMs).UTC().Format(time.RFC3339),
			"error":            r.Latest.Error,
			"execution_id":     r.Latest.ExecutionID,
			"scheduled_job_id": r.Latest.ScheduledJobID,
		}
		if len(r.Latest.Details) > 0 {
			detail["details"] = r.Latest.Details
		}
		if r.Latest.DurationMs > 0 {
			detail["duration_ms"] = r.Latest.DurationMs
		}
		return detail, true
	}
	return nil, false
}

// noteHandlerFailure records one failed handler execution for the row.
func (s *QNTXServer) noteHandlerFailure(f HandlerFailure) {
	if s == nil || s.handlerFailures == nil {
		return
	}
	if f.AtMs == 0 {
		f.AtMs = time.Now().UnixMilli()
	}
	// An unnamed handler is still a failing one. The job id is worse to read
	// than a name and better than an empty span on the row.
	if f.Handler == "" {
		f.Handler = f.ScheduledJobID
	}
	if f.Handler == "" {
		f.Handler = "handler"
	}
	s.handlerFailures.record(f)
}
