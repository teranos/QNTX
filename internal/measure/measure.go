// Package measure is the node's numbers.
//
// A log says what happened once. A metric is a number watched over time: how
// many, how deep, how long. The two answer different questions and the second
// one cannot be reconstructed from the first without reading every line.
//
// Every number this node emits is named in this file. That is the point of the
// package — the whole set is one screen, rather than something found by
// grepping for a call.
//
// Nothing here needs a switch. Metrics ride the Sentry client that
// logger.AddSentryOutput starts, and with no client every call below is a
// method on a no-op that discards it. A node with no DSN pays nothing.
package measure

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/getsentry/sentry-go/attribute"
)

// The numbers.
const (
	// AttestationsWritten is the node's job, counted. One per attestation it
	// accepted and stored.
	AttestationsWritten = "qntx.attestations.written"

	// QueryTook and QueryReturned are the two halves of the attestation query:
	// how long it ran, and how much it answered with. A query that gets slower
	// while returning the same amount is a different problem from one that got
	// slower because it is answering with more.
	QueryTook     = "qntx.query.took"
	QueryReturned = "qntx.query.returned"

	// Admitted and Refused are the door. Both are counted here; the refused
	// caller still learns nothing — a refusal is the node's to know, not
	// theirs.
	Admitted = "qntx.admitted"
	Refused  = "qntx.refused"

	// QueueDepth is what Pulse is holding right now, and WorkersActive is how
	// many are working it off. Either one alone says less than the pair: deep
	// and busy is a node under load, deep and idle is a node that is stuck.
	QueueDepth    = "qntx.pulse.queue.depth"
	WorkersActive = "qntx.pulse.workers.active"
)

// The dimensions.
//
// A metric attribute is something you slice a number by, and every distinct
// value is its own series. A namespace or a level is few. An actor, a DID, a
// path or an ID is not, and each one costs. Nothing unbounded goes on this
// list, and the values a metric is sliced by are named here for the same
// reason the metrics are.
const (
	// AttrLevel is the rung the caller reached the node at: ROOT, SUPER, TOKEN,
	// ATTESTOR.
	AttrLevel = "level"

	// AttrOutcome is how it ended, in a word — a small closed set per metric,
	// never an error string.
	AttrOutcome = "outcome"
)

// Attr is what a call site builds a dimension with. It is Sentry's own builder,
// re-exported so that a call site names one package and not two.
type Attr = attribute.Builder

// String builds a dimension. Dimensions are words, not numbers — a number
// belongs in the metric, not in what slices it.
func String(key, value string) Attr { return attribute.String(key, value) }

// meter is nil until Start, and holds a no-op when no Sentry client was bound.
// Either way the calls below cost an atomic load.
var meter atomic.Pointer[sentry.Meter]

// Start binds the meter to whatever Sentry client is running. Call it after
// logger.AddSentryOutput; before it, and with no DSN at all, every metric call
// goes nowhere.
//
// Calling it twice replaces the meter rather than doubling it.
func Start() {
	m := sentry.NewMeter(context.Background())
	meter.Store(&m)
}

// FIXME: every call below returns silently when no meter is bound. A number
// that was never recorded and a number that was zero read the same.

// Count adds to a running total: an attestation written, a caller refused.
func Count(name string, n int64, attrs ...Attr) {
	m := meter.Load()
	if m == nil {
		return
	}
	(*m).Count(name, n, sentry.WithAttributes(permitted(attrs)...))
}

// Gauge records what a number is right now: a queue depth, a lock held.
func Gauge(name string, value float64, attrs ...Attr) {
	m := meter.Load()
	if m == nil {
		return
	}
	(*m).Gauge(name, value, sentry.WithAttributes(permitted(attrs)...))
}

// Took records how long something ran. Duration is the distribution nearly
// every call site wants, and taking a time.Duration is what keeps the unit
// from ever disagreeing with the number.
func Took(name string, d time.Duration, attrs ...Attr) {
	m := meter.Load()
	if m == nil {
		return
	}
	(*m).Distribution(name, float64(d.Milliseconds()),
		sentry.WithUnit(sentry.UnitMillisecond),
		sentry.WithAttributes(permitted(attrs)...))
}

// Sized records how big one occurrence was — rows returned by a query, jobs in
// a batch. Unitless on purpose: the name says what is being counted, and a
// wrong unit is worse than none.
func Sized(name string, n int, attrs ...Attr) {
	m := meter.Load()
	if m == nil {
		return
	}
	(*m).Distribution(name, float64(n), sentry.WithAttributes(permitted(attrs)...))
}

// A dimension goes out as it was written. A call site that puts an address in
// one is the fault, and replacing it here would hide that from the one reader
// who would have shown it.
func permitted(attrs []Attr) []Attr {
	if len(attrs) == 0 {
		return nil
	}
	return attrs
}
