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

	// AttestationWriteTook is how long a POST that stored one ran, from the
	// request arriving to the answer. Every write is here; only a slow one is
	// also a log line, which names the phase it spent it in.
	AttestationWriteTook = "qntx.attestations.write.took"

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

	// RouteUnmatched is a request no line in the reach table names at all —
	// it never reaches the door Admitted and Refused are counted at.
	// HandleStatic's catch-all is where these land: a stray path, not a
	// caller turned away from somewhere real.
	RouteUnmatched = "qntx.route.unmatched"

	// QueueDepth is what Pulse is holding right now, and WorkersActive is how
	// many are working it off. Either one alone says less than the pair: deep
	// and busy is a node under load, deep and idle is a node that is stuck.
	QueueDepth    = "qntx.pulse.queue.depth"
	WorkersActive = "qntx.pulse.workers.active"

	// AttestationsEvicted is the other side of AttestationsWritten: what the
	// store let go of to stay within its bounds. Written climbing while evicted
	// climbs with it is a store at capacity.
	AttestationsEvicted = "qntx.attestations.evicted"

	// Embedded is one per attestation given a vector, and Clustered is one per
	// vector placed, sliced by outcome: assigned to a cluster, or noise. Many
	// embedded and few assigned is a model that no longer fits its data.
	Embedded  = "qntx.embeddings.embedded"
	Clustered = "qntx.embeddings.clustered"

	// Woven is one per LLM call the node attested. It is the spend, counted.
	Woven = "qntx.llm.woven"

	// StaandArrivals is one per arrival a stand recorded (ADR-035), sliced by the
	// stand. The event and the page are caller-controlled and unbounded, so they
	// are not dimensions here — they live in the stand element's live fold instead.
	StaandArrivals = "qntx.staand.arrivals"

	// OpenedRefused is one per request a caller sent past the floor on a plugin
	// path a line opened to strangers, sliced by the route. A flood is this
	// number climbing, and what it cost is each one refused.
	OpenedRefused = "qntx.opened.refused"

	// BootSubsystemTook is how long each step of the boot ran, sliced by the
	// step. The store's floor is the store-proof step: one write against the
	// real location, taken on every start (ADR-024, The floor).
	BootSubsystemTook = "qntx.boot.subsystem.took"

	// StoreCompacted is how long a merge ran and StoreCompactedFiles is how
	// many files it replaced, both sliced by namespace. A merge rewrites what
	// the namespace holds, so these two are the price of a cheap read
	// (ADR-024, Compaction).
	StoreCompacted      = "qntx.store.compacted"
	StoreCompactedFiles = "qntx.store.compacted.files"

	// StoreCompactedBytes is how many bytes the file a merge wrote holds. The
	// merge rewrites the whole namespace, so this is what each one uploads.
	StoreCompactedBytes = "qntx.store.compacted.bytes"

	// StoreFiles is how many Parquet files a namespace's record holds, taken
	// after each send. ADR-024 names it as what a read of the record costs.
	StoreFiles = "qntx.store.files"

	// StoreRequests is what a namespace's record asked its location for,
	// sliced by namespace and by which request it was. ADR-024 prices a read
	// in files; S3 prices one in requests, and the node could say the first
	// number and not the second. A location on this filesystem asks for none.
	StoreRequests = "qntx.store.requests"

	// StoreUnsent is how many attestations a landing file holds that the
	// record does not yet have: what losing the host right now would lose
	// (ADR-037). It climbs between sends and falls when one lands.
	StoreUnsent = "qntx.store.unsent"

	// StoreTakenIn is how long opening a namespace spent reading its record
	// into the landing file, and StoreTakenInRows is how many attestations the
	// record answered with, sliced by namespace and by whether it was the
	// whole record. A whole take-in holds the namespace in memory at once.
	StoreTakenIn     = "qntx.store.taken_in"
	StoreTakenInRows = "qntx.store.taken_in.rows"

	// StoreSent is how long a send from a landing file to the record ran, and
	// StoreSentRows is how many attestations it carried, both sliced by
	// namespace. One sample per send that carried anything; a send writes one
	// file per 5000 rows, so below that their count is the files the record
	// was handed (ADR-037).
	StoreSent     = "qntx.store.sent"
	StoreSentRows = "qntx.store.sent.rows"

	// HostCPU, HostMemory and HostDisk are the machine under the node, each as
	// a percentage of what it has. Dilation reads the first two and throws
	// them away; nothing read the disk at all.
	HostCPU    = "qntx.host.cpu"
	HostMemory = "qntx.host.memory"
	HostDisk   = "qntx.host.disk"

	// HostNetIn and HostNetOut are bytes a second across every interface. The
	// kernel counts from boot, and a total since boot answers nothing.
	HostNetIn  = "qntx.host.net.in"
	HostNetOut = "qntx.host.net.out"

	// HostSwap is how much of swap is in use, HostSwapIn and HostSwapOut the
	// bytes a second through it. Swap full and still costs nothing; swap read
	// back is the machine out of memory.
	HostSwap    = "qntx.host.swap"
	HostSwapIn  = "qntx.host.swap.in"
	HostSwapOut = "qntx.host.swap.out"
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

	// AttrStand is which stand an arrival landed on: its key, market/slug. Bounded
	// because ROOT names the stands.
	AttrStand = "stand"

	// AttrEvent is the stand event, staand:page_view and the like. The pixel side
	// names it, so it is bounded not by nature but by the caller: only a stand's
	// first several distinct events keep their name, the rest fold to "other"
	// before this is set (ADR-035), so an invented event cannot grow the series.
	AttrEvent = "event"

	// AttrSubsystem is which boot step: the names in server/subsystem.go, ten of them.
	AttrSubsystem = "subsystem"

	// AttrRoute is the path a request was gated on, as the reach table names
	// it — server/reach/table.go's own quoted patterns, not r.URL.Path.
	// Bounded because the table is: a few dozen lines, not a caller-chosen
	// string.
	AttrRoute = "route"

	// AttrStore is which namespace's store: system, default, and the ones ROOT
	// creates. Bounded because a namespace is created, not arrived at.
	AttrStore = "store"

	// AttrRequest is which request a store made of its location: PUT, GET,
	// HEAD, LIST or DELETE. Bounded: the five an S3 client makes, named in
	// crates/ats-duckdb/src/objects.rs.
	AttrRequest = "request"

	// AttrOf is what the request was for, in the words make parity uses for
	// its rows: access_tokens, attestations, schedule_ticks, watchers. What a
	// count without it cannot say is which reader to go and fix.
	AttrOf = "of"

	// AttrMethod is the request's HTTP verb. Bounded: a handful of methods,
	// never a caller-chosen string.
	AttrMethod = "method"

	// AttrWhole is whether a take-in read the whole record or from its mark
	// on: "true" or "false".
	AttrWhole = "whole"
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
