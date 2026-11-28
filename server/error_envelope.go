package server

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	errors "github.com/teranos/sacred-error"
)

// ErrorEnvelope is the one shape a failure takes on its way out of QNTX,
// whether it leaves over HTTP or over the websocket (ADR: tsot-roam ERROR.md).
type ErrorEnvelope struct {
	// ID is stable for the life of this failure, so the same error rendered
	// inline and in a log panel is keyed to one thing.
	ID string `json:"id"`

	// Surface names where it originated. Without it an error is unactionable:
	// the renderer has nowhere to put it and the reader has nothing to look at.
	Surface string `json:"surface,omitempty"`

	Error   string   `json:"error"`
	Details []string `json:"details,omitempty"`
	Hints   []string `json:"hints,omitempty"`

	Timestamp int64 `json:"timestamp"`
}

var errorSeq atomic.Uint64

// newErrorEnvelope builds the envelope from a typed error, keeping whatever
// context the chain carries rather than flattening it to a sentence.
func newErrorEnvelope(surface string, err error) ErrorEnvelope {
	return ErrorEnvelope{
		ID:        fmt.Sprintf("ERR-%d-%d", time.Now().UnixMilli(), errorSeq.Add(1)),
		Surface:   surface,
		Error:     err.Error(),
		Details:   errors.GetAllDetails(err),
		Hints:     errors.GetAllHints(err),
		Timestamp: time.Now().Unix(),
	}
}

// "and what get's sent to sentry or anything else , is the user experience of an error,"
// "something that makes it easy for us to see and fix, because the experience being bad is immediatly clear and why"
// 「[Opus 5.5] seen is the envelope as sacred-error's Error: what the person was answered」
// 「[Opus 5.5] with, under the same id, is what is reported. The wire becomes this value」
// 「[Opus 5.5] itself once QNTX-Web reads it.」
func (e ErrorEnvelope) seen() errors.Error {
	why := append([]string{}, e.Details...)
	for _, hint := range e.Hints {
		why = append(why, "hint: "+hint)
	}
	return errors.Error{
		ID:       e.ID,
		Severity: errors.Err,
		Context:  errors.Context{Surface: e.Surface},
		Title:    e.Error,
		Why:      strings.Join(why, "\n"),
		At:       time.Unix(e.Timestamp, 0).UTC().Format(time.RFC3339),
		Source:   "qntx",
	}
}
