package async

import (
	"database/sql"
	"encoding/json"

	"time"

	"github.com/teranos/errors"
)

// "nil is nil"
//
// A job says when it started and completed, or that it has not yet, and it
// says the pulse state it carries, or that it carries none. Neither is held
// by a nil: a job naming neither side is refused where it is kept or sent.

// momentKind is which side a Moment names.
type momentKind int

const (
	momentAt momentKind = iota + 1
	momentNotYet
)

// Moment is when something happened to a job, or that it has not yet.
type Moment struct {
	kind momentKind
	at   time.Time
}

// At is a Moment that happened at t.
func At(t time.Time) Moment {
	return Moment{kind: momentAt, at: t}
}

// NotYet is a Moment that has not happened.
func NotYet() Moment {
	return Moment{kind: momentNotYet}
}

// When is when it happened and true, or false for one that has not yet. A
// Moment naming neither is refused, with what names it.
func (m Moment) When(what string) (time.Time, bool, error) {
	switch m.kind {
	case momentAt:
		return m.at, true, nil
	case momentNotYet:
		return time.Time{}, false, nil
	}
	return time.Time{}, false, errors.Newf("%s does not say when, or that it has not yet", what)
}

// Time is when it happened, or why there is no time: it has not yet, or the
// Moment names neither.
func (m Moment) Time(what string) (time.Time, error) {
	at, happened, err := m.When(what)
	if err != nil {
		return time.Time{}, err
	}
	if !happened {
		return time.Time{}, errors.Newf("%s has not happened yet", what)
	}
	return at, nil
}

// column is a Moment as its column holds it: NULL for one that has not yet.
func (m Moment) column(what string) (sql.NullTime, error) {
	at, happened, err := m.When(what)
	return sql.NullTime{Time: at, Valid: happened}, err
}

// momentOfColumn is the Moment a column holds: NULL is one that has not yet.
func momentOfColumn(column sql.NullTime) Moment {
	if column.Valid {
		return At(column.Time)
	}
	return NotYet()
}

// pulseKind is which side a Pulse names.
type pulseKind int

const (
	pulseHeld pulseKind = iota + 1
	pulseNone
)

// Pulse is the pulse rate limiting state a job carries, or that it carries
// none.
type Pulse struct {
	kind  pulseKind
	state PulseState
}

// Pulsed is a job carrying a pulse state.
func Pulsed(state PulseState) Pulse {
	return Pulse{kind: pulseHeld, state: state}
}

// NoPulse is a job carrying no pulse state.
func NoPulse() Pulse {
	return Pulse{kind: pulseNone}
}

// State is the pulse state and true, or false for a job carrying none. A
// Pulse naming neither is refused, with what names it.
func (p Pulse) State(what string) (PulseState, bool, error) {
	switch p.kind {
	case pulseHeld:
		return p.state, true, nil
	case pulseNone:
		return PulseState{}, false, nil
	}
	return PulseState{}, false, errors.Newf("%s does not say its pulse state, or that it carries none", what)
}

// paused is the Pulse with its state paused for reason; a job carrying none
// still carries none.
func (p Pulse) paused(paused bool, reason string) Pulse {
	switch p.kind {
	case pulseHeld:
		state := p.state
		state.IsPaused = paused
		state.PauseReason = reason
		return Pulsed(state)
	}
	// Carrying none, or naming neither, it is as it was: where the job is
	// kept or sent, naming neither is refused.
	return p
}

// column is a Pulse as its column holds it: the state as JSON, or NULL for a
// job carrying none.
func (p Pulse) column(what string) (sql.NullString, error) {
	state, held, err := p.State(what)
	if err != nil {
		return sql.NullString{}, err
	}
	if !held {
		return sql.NullString{}, nil
	}
	data, err := json.Marshal(state)
	if err != nil {
		return sql.NullString{}, errors.Wrapf(err, "failed to marshal the pulse state of %s", what)
	}
	return sql.NullString{String: string(data), Valid: true}, nil
}
