package server

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/teranos/errors"
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
