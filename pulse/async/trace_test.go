package async

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// ============================================================================
// The trace crosses the queue.
//
// A job is enqueued in one context and run later by a worker in another, so the
// span that caused it is finished by the time it runs. These are about the
// cause surviving that gap — a child job landing in its parent's trace instead
// of arriving with nothing above it.
// ============================================================================

// enqueueingHandler is a parent: while running, it enqueues a child through the
// context it was given. That context is the one carrying the parent's span.
type enqueueingHandler struct {
	name  string
	queue *Queue
	child string
}

func (h *enqueueingHandler) Name() string { return h.name }

func (h *enqueueingHandler) Execute(ctx context.Context, job *Job) error {
	child, err := createTestJob(h.child, "child-of-"+job.ID, 1, 0)
	if err != nil {
		return err
	}
	child.ParentJobID = job.ID
	return h.queue.EnqueueContext(ctx, child)
}

// traceIDOf reads which trace a transaction belongs to.
func traceIDOf(t *testing.T, event *sentry.Event) string {
	t.Helper()
	return fmt.Sprintf("%v", traceContext(t, event)["trace_id"])
}

func spanIDOf(t *testing.T, event *sentry.Event) string {
	t.Helper()
	return fmt.Sprintf("%v", traceContext(t, event)["span_id"])
}

// One tree per originating action. This is the whole of it.
func TestAChildJobContinuesItsParentsTrace(t *testing.T) {
	held := shipping(t, 1.0)

	db := qntxtest.CreateTestDB(t)
	registry := NewHandlerRegistry()
	noPolling := time.Duration(0)
	pool := NewWorkerPoolWithRegistry(
		context.Background(), db, createTestConfig(),
		WorkerPoolConfig{Workers: 1, PollInterval: &noPolling},
		createTestLogger(), registry, nil, nil,
	)
	registry.Register(&enqueueingHandler{name: "parent.work", queue: pool.queue, child: "child.work"})
	registry.Register(&countingHandler{name: "child.work"})

	parent, err := createTestJob("parent.work", "span-test", 1, 0)
	if err != nil {
		t.Fatalf("could not build the parent job: %v", err)
	}
	if err := pool.queue.Enqueue(parent); err != nil {
		t.Fatalf("could not enqueue the parent: %v", err)
	}

	// The parent runs and enqueues the child; then the child runs, on a later
	// pass, from a row — which is the gap this is about.
	if err := pool.processNextJob(); err != nil {
		t.Fatalf("parent execution: %v", err)
	}
	if err := pool.processNextJob(); err != nil {
		t.Fatalf("child execution: %v", err)
	}

	shipped := held.transactions()
	if len(shipped) != 2 {
		t.Fatalf("shipped %d transactions, want 2 (the parent and the child)", len(shipped))
	}

	var parentEvent, childEvent *sentry.Event
	for _, e := range shipped {
		switch e.Transaction {
		case "parent.work":
			parentEvent = e
		case "child.work":
			childEvent = e
		}
	}
	if parentEvent == nil || childEvent == nil {
		t.Fatalf("wanted a parent.work and a child.work transaction, got %q and %q",
			shipped[0].Transaction, shipped[1].Transaction)
	}

	if traceIDOf(t, childEvent) != traceIDOf(t, parentEvent) {
		t.Errorf("the child is in trace %s and the parent in %s — two traces, not one",
			traceIDOf(t, childEvent), traceIDOf(t, parentEvent))
	}

	childsParent, ok := traceContext(t, childEvent)["parent_span_id"]
	if !ok {
		t.Fatal("the child transaction carries no parent_span_id, so it is a root")
	}
	if fmt.Sprintf("%v", childsParent) != spanIDOf(t, parentEvent) {
		t.Errorf("the child hangs off span %v, not off the parent's %s",
			childsParent, spanIDOf(t, parentEvent))
	}
}

// The cause is written where the job is created and read where it is executed.
// Between those two the row is updated repeatedly — dequeued, started,
// re-queued — and the cause has to survive every one of them.
func TestTheCauseSurvivesTheQueue(t *testing.T) {
	shipping(t, 1.0)

	db := qntxtest.CreateTestDB(t)
	queue := NewQueue(db)

	span := sentry.StartTransaction(
		sentry.SetHubOnContext(context.Background(), sentry.CurrentHub().Clone()),
		"the.cause",
	)
	job, err := createTestJob("caused.work", "span-test", 1, 0)
	if err != nil {
		t.Fatalf("could not build a job: %v", err)
	}
	if err := queue.EnqueueContext(span.Context(), job); err != nil {
		t.Fatalf("could not enqueue: %v", err)
	}
	span.Finish()

	stored, err := queue.GetJob(job.ID)
	if err != nil {
		t.Fatalf("could not read the job back: %v", err)
	}
	if stored.TraceContext == "" {
		t.Fatal("the job was enqueued inside a span and stored no cause")
	}
	if !strings.HasPrefix(stored.TraceContext, string(span.TraceID.Hex())) {
		t.Errorf("the stored cause %q does not name the trace %s it was enqueued in",
			stored.TraceContext, string(span.TraceID.Hex()))
	}

	// Dequeue updates the row. Anything writing the trace columns here would
	// erase the cause between the enqueue and the execution.
	dequeued, err := queue.Dequeue()
	if err != nil {
		t.Fatalf("could not dequeue: %v", err)
	}
	if dequeued.TraceContext != stored.TraceContext {
		t.Errorf("the cause changed across the dequeue: %q became %q",
			stored.TraceContext, dequeued.TraceContext)
	}

	// And the re-queue, which hands UpdateJob a Job the worker has been
	// mutating — the caller most able to erase what it never set.
	dequeued.Status = JobStatusQueued
	if err := queue.UpdateJob(dequeued); err != nil {
		t.Fatalf("could not re-queue: %v", err)
	}
	again, err := queue.GetJob(job.ID)
	if err != nil {
		t.Fatalf("could not read the job back after the re-queue: %v", err)
	}
	if again.TraceContext != stored.TraceContext {
		t.Errorf("the re-queue erased the cause: %q became %q",
			stored.TraceContext, again.TraceContext)
	}
}

// A child created where the parent's span cannot be reached — another process,
// holding only the parent's id, which is all a plugin enqueueing over gRPC has
// — still lands in the parent's trace. The parent left its execution span on
// its row, and the row is what the child reads.
func TestAChildWithOnlyAParentIDStillJoinsTheTrace(t *testing.T) {
	held := shipping(t, 1.0)

	pool := poolWith(t, &countingHandler{name: "parent.work"})
	parent := enqueue(t, pool, "parent.work")

	if err := pool.processNextJob(); err != nil {
		t.Fatalf("parent execution: %v", err)
	}

	ran, err := pool.queue.GetJob(parent.ID)
	if err != nil {
		t.Fatalf("could not read the parent back: %v", err)
	}
	if ran.ExecTraceContext == "" {
		t.Fatal("the parent ran under a sampled span and left nothing on its row")
	}

	// Everything the child gets is the parent's id. No context, no span —
	// exactly what arrives over gRPC.
	child, err := createTestJob("child.work", "over-grpc", 1, 0)
	if err != nil {
		t.Fatalf("could not build the child: %v", err)
	}
	child.ParentJobID = parent.ID
	if err := pool.queue.Enqueue(child); err != nil {
		t.Fatalf("could not enqueue the child: %v", err)
	}

	stored, err := pool.queue.GetJob(child.ID)
	if err != nil {
		t.Fatalf("could not read the child back: %v", err)
	}
	if stored.TraceContext != ran.ExecTraceContext {
		t.Errorf("the child inherited %q, not the parent's execution span %q",
			stored.TraceContext, ran.ExecTraceContext)
	}

	// And it holds when the child actually runs.
	pool.Registry().Register(&countingHandler{name: "child.work"})
	if err := pool.processNextJob(); err != nil {
		t.Fatalf("child execution: %v", err)
	}

	shipped := held.transactions()
	if len(shipped) != 2 {
		t.Fatalf("shipped %d transactions, want 2", len(shipped))
	}
	if traceIDOf(t, shipped[0]) != traceIDOf(t, shipped[1]) {
		t.Errorf("the child ran in trace %s and the parent in %s — two traces, not one",
			traceIDOf(t, shipped[1]), traceIDOf(t, shipped[0]))
	}
}

// With tracing off there is no span worth recording, and every job would
// otherwise pay for an UPDATE nobody reads.
func TestNoTracingWritesNoExecutionSpan(t *testing.T) {
	shipping(t, 0)
	pool := poolWith(t, &countingHandler{name: "quiet.work"})
	job := enqueue(t, pool, "quiet.work")

	if err := pool.processNextJob(); err != nil {
		t.Fatalf("processNextJob: %v", err)
	}

	ran, err := pool.queue.GetJob(job.ID)
	if err != nil {
		t.Fatalf("could not read the job back: %v", err)
	}
	if ran.ExecTraceContext != "" {
		t.Errorf("tracing is off and the row carries an execution span: %q", ran.ExecTraceContext)
	}
}

// A job nothing in particular asked for is head of its own trace. Enqueue with
// no context is that case, and it is most of the queue: the ticker enqueues
// scheduled work, and a schedule is not a cause to hang a trace from.
func TestAnUncausedJobIsItsOwnRoot(t *testing.T) {
	held := shipping(t, 1.0)
	pool := poolWith(t, &countingHandler{name: "uncaused.work"})
	job := enqueue(t, pool, "uncaused.work")

	stored, err := pool.queue.GetJob(job.ID)
	if err != nil {
		t.Fatalf("could not read the job back: %v", err)
	}
	if stored.TraceContext != "" {
		t.Errorf("a job enqueued outside a span stored a cause: %q", stored.TraceContext)
	}

	if err := pool.processNextJob(); err != nil {
		t.Fatalf("processNextJob: %v", err)
	}

	shipped := held.transactions()
	if len(shipped) != 1 {
		t.Fatalf("shipped %d transactions, want 1", len(shipped))
	}
	if _, hasParent := traceContext(t, shipped[0])["parent_span_id"]; hasParent {
		t.Error("an uncaused job hangs off a parent span it never had")
	}
}
