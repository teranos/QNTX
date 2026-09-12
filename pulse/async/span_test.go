package async

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/errors"
)

// ============================================================================
// The duration writeTaskLog records as "Completed in 4821ms" is prose: it
// answers one run. These tests are about the same number leaving as a span,
// where it answers p95 per handler instead.
// ============================================================================

// heldTransport keeps what would have been shipped, so a test can read it
// instead of the Trace Explorer.
type heldTransport struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (t *heldTransport) Configure(sentry.ClientOptions) {}
func (t *heldTransport) Close()                         {}
func (t *heldTransport) Flush(time.Duration) bool       { return true }

func (t *heldTransport) FlushWithContext(context.Context) bool { return true }

func (t *heldTransport) SendEvent(event *sentry.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, event)
}

func (t *heldTransport) transactions() []*sentry.Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []*sentry.Event
	for _, e := range t.events {
		if e.Type == "transaction" {
			out = append(out, e)
		}
	}
	return out
}

// shipping points the SDK at a transport this test can read, at the given
// sample rate, and puts the hub back when the test ends. The hub is global, so
// restoring it is what keeps these tests from reaching each other.
func shipping(t *testing.T, rate float64) *heldTransport {
	t.Helper()

	held := &heldTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:              "https://key@o0.ingest.sentry.io/1",
		Transport:        held,
		EnableTracing:    rate > 0,
		TracesSampleRate: rate,
	})
	if err != nil {
		t.Fatalf("could not build a Sentry client: %v", err)
	}

	// startJobSpan clones the current hub, so binding here is what the worker
	// picks up. The previous client goes back afterwards: the hub is global,
	// and without this one test's client would be the next test's.
	previous := sentry.CurrentHub().Client()
	sentry.CurrentHub().BindClient(client)
	t.Cleanup(func() { sentry.CurrentHub().BindClient(previous) })

	return held
}

// countingHandler is a handler that runs, spends, and reports progress — the
// three things the span is supposed to carry out of it.
type countingHandler struct {
	name string
	cost float64
	done int
	err  error
}

func (h *countingHandler) Name() string { return h.name }

func (h *countingHandler) Execute(ctx context.Context, job *Job) error {
	job.RecordCost(h.cost)
	job.UpdateProgress(h.done)
	return h.err
}

// poolWith builds a pool holding one handler, with the gates off, so what a
// test sees is the execution and nothing around it.
func poolWith(t *testing.T, handler JobHandler) *WorkerPool {
	t.Helper()

	db := qntxtest.CreateTestDB(t)
	registry := NewHandlerRegistry()
	registry.Register(handler)

	noPolling := time.Duration(0)
	pool := NewWorkerPoolWithRegistry(
		context.Background(),
		db,
		createTestConfig(),
		WorkerPoolConfig{Workers: 1, PollInterval: &noPolling},
		createTestLogger(),
		registry,
		nil, // no budget tracker: the budget gate is not what these tests are about
		nil, // no rate limiter, same reason
	)
	return pool
}

// enqueue puts one job in front of the worker.
func enqueue(t *testing.T, pool *WorkerPool, handlerName string) *Job {
	t.Helper()

	job, err := createTestJob(handlerName, "span-test", 10, 0.25)
	if err != nil {
		t.Fatalf("could not build a job: %v", err)
	}
	if err := pool.queue.Enqueue(job); err != nil {
		t.Fatalf("could not enqueue job %s: %v", job.ID, err)
	}
	return job
}

// traceContext is the map the transaction event carries op, status and data in.
func traceContext(t *testing.T, event *sentry.Event) map[string]interface{} {
	t.Helper()

	trace, ok := event.Contexts["trace"]
	if !ok {
		t.Fatal("the transaction carries no trace context")
	}
	return trace
}

func traceData(t *testing.T, event *sentry.Event) map[string]interface{} {
	t.Helper()

	data, ok := traceContext(t, event)["data"].(map[string]interface{})
	if !ok {
		t.Fatal("the trace context carries no data")
	}
	return data
}

// One execution is one transaction, and it is named by the handler that ran.
func TestAnExecutionShipsAsOneTransaction(t *testing.T) {
	held := shipping(t, 1.0)
	pool := poolWith(t, &countingHandler{name: "data.batch-import"})
	enqueue(t, pool, "data.batch-import")

	if err := pool.processNextJob(); err != nil {
		t.Fatalf("processNextJob: %v", err)
	}

	shipped := held.transactions()
	if len(shipped) != 1 {
		t.Fatalf("one execution shipped %d transactions, want 1", len(shipped))
	}

	event := shipped[0]
	if event.Transaction != "data.batch-import" {
		t.Errorf("the transaction is named %q, not after its handler", event.Transaction)
	}
	if op := traceContext(t, event)["op"]; op != "queue.process" {
		t.Errorf("op is %v, want queue.process — the op is what the Explorer groups Pulse by", op)
	}
}

// The point of the whole exercise: a duration that is a number.
func TestTheTransactionCarriesADuration(t *testing.T) {
	held := shipping(t, 1.0)
	pool := poolWith(t, &countingHandler{name: "slow.thing"})
	enqueue(t, pool, "slow.thing")

	if err := pool.processNextJob(); err != nil {
		t.Fatalf("processNextJob: %v", err)
	}

	shipped := held.transactions()
	if len(shipped) != 1 {
		t.Fatalf("shipped %d transactions, want 1", len(shipped))
	}

	event := shipped[0]
	if event.StartTime.IsZero() {
		t.Fatal("the transaction has no start time")
	}
	if event.Timestamp.IsZero() {
		t.Fatal("the transaction has no end time, so it has no duration")
	}
	if event.Timestamp.Before(event.StartTime) {
		t.Errorf("the transaction ends (%v) before it starts (%v)", event.Timestamp, event.StartTime)
	}
}

// Cost and progress are written through the *Job the handler was given, so a
// span that read them at the start would ship zeros.
func TestCostAndProgressAreReadAfterTheHandlerRan(t *testing.T) {
	held := shipping(t, 1.0)
	pool := poolWith(t, &countingHandler{name: "spendy", cost: 1.50, done: 7})
	job := enqueue(t, pool, "spendy")

	if err := pool.processNextJob(); err != nil {
		t.Fatalf("processNextJob: %v", err)
	}

	shipped := held.transactions()
	if len(shipped) != 1 {
		t.Fatalf("shipped %d transactions, want 1", len(shipped))
	}

	data := traceData(t, shipped[0])
	if data["job.cost_actual"] != 1.50 {
		t.Errorf("cost_actual is %v, want 1.5 — read before the handler spent it", data["job.cost_actual"])
	}
	if data["job.progress_current"] != 7 {
		t.Errorf("progress_current is %v, want 7", data["job.progress_current"])
	}
	if data["messaging.message.id"] != job.ID {
		t.Errorf("the transaction names job %v, not %s", data["messaging.message.id"], job.ID)
	}
	if data["job.cost_estimate"] != 0.25 {
		t.Errorf("cost_estimate is %v, want 0.25", data["job.cost_estimate"])
	}
}

// A handler that returns an error is not-OK, and says which kind of not-OK it
// was. Re-queued because plugins had not loaded is also not-OK and is not the
// same thing to look at.
func TestAFailedExecutionIsMarkedFailed(t *testing.T) {
	held := shipping(t, 1.0)
	pool := poolWith(t, &countingHandler{name: "breaks", err: errors.New("the handler said no")})
	enqueue(t, pool, "breaks")

	if err := pool.processNextJob(); err != nil {
		t.Fatalf("processNextJob: %v", err)
	}

	shipped := held.transactions()
	if len(shipped) != 1 {
		t.Fatalf("shipped %d transactions, want 1", len(shipped))
	}

	event := shipped[0]
	if status := traceContext(t, event)["status"]; status != sentry.SpanStatusInternalError {
		t.Errorf("status is %v, want internal_error", status)
	}
	data := traceData(t, event)
	if data["pulse.outcome"] != "failed" {
		t.Errorf("outcome is %v, want failed", data["pulse.outcome"])
	}
	if data["error"] != "the handler said no" {
		t.Errorf("the transaction does not carry what the handler said, got %v", data["error"])
	}
}

// A job whose handler has not been registered is re-queued, not failed. That is
// a different outcome from a handler that ran and returned an error, and a
// failure rate that counted them together would alarm on plugins booting.
func TestAMissingHandlerIsItsOwnOutcome(t *testing.T) {
	held := shipping(t, 1.0)
	pool := poolWith(t, &countingHandler{name: "registered.one"})
	enqueue(t, pool, "nobody.registered.this")

	if err := pool.processNextJob(); err != nil {
		t.Fatalf("processNextJob: %v", err)
	}

	shipped := held.transactions()
	if len(shipped) != 1 {
		t.Fatalf("shipped %d transactions, want 1", len(shipped))
	}

	event := shipped[0]
	if status := traceContext(t, event)["status"]; status != sentry.SpanStatusUnavailable {
		t.Errorf("status is %v, want unavailable", status)
	}
	if outcome := traceData(t, event)["pulse.outcome"]; outcome != "requeued_no_handler" {
		t.Errorf("outcome is %v, want requeued_no_handler", outcome)
	}
}

// Zero means zero. A node that has said nothing about tracing ships no spans,
// and the job still runs.
func TestNoSampleRateShipsNoSpans(t *testing.T) {
	held := shipping(t, 0)
	pool := poolWith(t, &countingHandler{name: "quiet.work"})
	job := enqueue(t, pool, "quiet.work")

	if err := pool.processNextJob(); err != nil {
		t.Fatalf("processNextJob: %v", err)
	}

	if shipped := held.transactions(); len(shipped) != 0 {
		t.Errorf("tracing is off and %d transactions were shipped", len(shipped))
	}

	ran, err := pool.queue.GetJob(job.ID)
	if err != nil {
		t.Fatalf("could not read back job %s: %v", job.ID, err)
	}
	if ran.Status != JobStatusCompleted {
		t.Errorf("job is %s, not completed — turning tracing off changed what ran", ran.Status)
	}
}

// The handler runs under the span's context, so a span a handler opens becomes
// a child of the job rather than a root of its own. This is what Stage 2 needs
// already being true inside one process.
func TestAHandlerSpanNestsUnderTheJob(t *testing.T) {
	held := shipping(t, 1.0)

	var nested *sentry.Span
	pool := poolWith(t, &nestingHandler{name: "nests", onRun: func(ctx context.Context) {
		nested = sentry.StartSpan(ctx, "db.query")
		nested.Finish()
	}})
	enqueue(t, pool, "nests")

	if err := pool.processNextJob(); err != nil {
		t.Fatalf("processNextJob: %v", err)
	}

	shipped := held.transactions()
	if len(shipped) != 1 {
		t.Fatalf("shipped %d transactions, want 1 — a nested span shipped separately", len(shipped))
	}
	if nested == nil {
		t.Fatal("the handler never ran")
	}
	if len(shipped[0].Spans) != 1 {
		t.Fatalf("the transaction carries %d child spans, want 1", len(shipped[0].Spans))
	}
	if shipped[0].Spans[0].Op != "db.query" {
		t.Errorf("the child span is %q, not the one the handler opened", shipped[0].Spans[0].Op)
	}
}

type nestingHandler struct {
	name  string
	onRun func(ctx context.Context)
}

func (h *nestingHandler) Name() string { return h.name }

func (h *nestingHandler) Execute(ctx context.Context, job *Job) error {
	h.onRun(ctx)
	return nil
}
