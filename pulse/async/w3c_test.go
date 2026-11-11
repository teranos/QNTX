package async

import (
	"context"
	"fmt"
	"testing"

	"github.com/getsentry/sentry-go"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// 「[Opus 5.5] The four trace columns on a job row hold W3C trace context: the」
// 「[Opus 5.5] context columns a traceparent, the baggage columns a baggage header.」

// 「[Opus 5.5] parsed reads a column the way a W3C receiver reads a traceparent header.」
func parsed(t *testing.T, traceparent string) trace.SpanContext {
	t.Helper()
	ctx := propagation.TraceContext{}.Extract(context.Background(),
		propagation.MapCarrier{"traceparent": traceparent})
	return trace.SpanContextFromContext(ctx)
}

// 「[Opus 5.5] recording makes OpenTelemetry's global provider one that keeps its」
// 「[Opus 5.5] ended spans in memory, and puts the previous one back after the test.」
func recording(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	return recorder
}

func TestTheCauseIsStoredAsATraceparent(t *testing.T) {
	shipping(t, 1.0)
	queue := NewQueue(qntxtest.CreateTestDB(t))

	span := sentry.StartTransaction(
		sentry.SetHubOnContext(context.Background(), sentry.CurrentHub().Clone()),
		"the.cause",
	)
	job, err := createTestJob("caused.work", "w3c-test", 1)
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
	want := fmt.Sprintf("00-%s-%s-01", span.TraceID, span.SpanID)
	if stored.TraceContext != want {
		t.Errorf("trace_context is %q, want the traceparent %q", stored.TraceContext, want)
	}
	if _, err := baggage.Parse(stored.TraceBaggage); err != nil || stored.TraceBaggage == "" {
		t.Errorf("trace_baggage %q is not a W3C baggage header: %v", stored.TraceBaggage, err)
	}
}

func TestTheExecutionSpanIsLeftAsATraceparent(t *testing.T) {
	held := shipping(t, 1.0)
	pool := poolWith(t, &countingHandler{name: "parent.work"})
	job := enqueue(t, pool, "parent.work")

	if err := pool.processNextJob(); err != nil {
		t.Fatalf("processNextJob: %v", err)
	}
	ran, err := pool.queue.GetJob(job.ID)
	if err != nil {
		t.Fatalf("could not read the job back: %v", err)
	}

	sc := parsed(t, ran.ExecTraceContext)
	if !sc.IsValid() {
		t.Fatalf("exec_trace_context %q is not a W3C traceparent", ran.ExecTraceContext)
	}
	shipped := held.transactions()
	if len(shipped) != 1 {
		t.Fatalf("shipped %d transactions, want 1", len(shipped))
	}
	if sc.TraceID().String() != traceIDOf(t, shipped[0]) || sc.SpanID().String() != spanIDOf(t, shipped[0]) {
		t.Errorf("exec_trace_context names %s/%s, the execution was %s/%s",
			sc.TraceID(), sc.SpanID(), traceIDOf(t, shipped[0]), spanIDOf(t, shipped[0]))
	}
	if _, err := baggage.Parse(ran.ExecTraceBaggage); err != nil || ran.ExecTraceBaggage == "" {
		t.Errorf("exec_trace_baggage %q is not a W3C baggage header: %v", ran.ExecTraceBaggage, err)
	}
}

// 「[Opus 5.5] A row written before the change holds Sentry's layout, which is not a」
// 「[Opus 5.5] traceparent: the job it belongs to runs as the head of a trace of its own.」
func TestARowInSentrysLayoutIsNoCause(t *testing.T) {
	held := shipping(t, 1.0)
	pool := poolWith(t, &countingHandler{name: "old.work"})

	job, err := createTestJob("old.work", "w3c-test", 1)
	if err != nil {
		t.Fatalf("could not build a job: %v", err)
	}
	job.TraceContext = "0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-1"
	if err := pool.queue.Enqueue(job); err != nil {
		t.Fatalf("could not enqueue: %v", err)
	}
	if err := pool.processNextJob(); err != nil {
		t.Fatalf("processNextJob: %v", err)
	}

	shipped := held.transactions()
	if len(shipped) != 1 {
		t.Fatalf("shipped %d transactions, want 1", len(shipped))
	}
	if traceIDOf(t, shipped[0]) == "0af7651916cd43dd8448eb211c80319c" {
		t.Error("a row in Sentry's layout was continued")
	}
	if _, hasParent := traceContext(t, shipped[0])["parent_span_id"]; hasParent {
		t.Error("a row in Sentry's layout gave the execution a parent")
	}
}

// 「[Opus 5.5] With spans leaving over OTLP the innermost span of a running job is its」
// 「[Opus 5.5] OpenTelemetry span, and that is what its row and its children name.」
func TestWithOTelTheChildHangsFromTheParentsOTelSpan(t *testing.T) {
	held := shipping(t, 1.0)
	spans := recording(t)

	pool := poolWith(t, &countingHandler{name: "unused"})
	pool.Registry().Register(&enqueueingHandler{name: "parent.work", queue: pool.queue, child: "child.work"})
	pool.Registry().Register(&countingHandler{name: "child.work"})
	parent := enqueue(t, pool, "parent.work")

	if err := pool.processNextJob(); err != nil {
		t.Fatalf("parent execution: %v", err)
	}
	if err := pool.processNextJob(); err != nil {
		t.Fatalf("child execution: %v", err)
	}

	ended := spans.Ended()
	byName := map[string]sdktrace.ReadOnlySpan{}
	for _, s := range ended {
		byName[s.Name()] = s
	}
	parentOTel, childOTel := byName["process parent.work"], byName["process child.work"]
	if parentOTel == nil || childOTel == nil {
		t.Fatalf("wanted both job spans, got %d spans", len(ended))
	}

	ran, err := pool.queue.GetJob(parent.ID)
	if err != nil {
		t.Fatalf("could not read the parent back: %v", err)
	}
	if sc := parsed(t, ran.ExecTraceContext); sc.SpanID() != parentOTel.SpanContext().SpanID() {
		t.Errorf("the parent's row names span %s, its OpenTelemetry span is %s",
			sc.SpanID(), parentOTel.SpanContext().SpanID())
	}

	if childOTel.SpanContext().TraceID() != parentOTel.SpanContext().TraceID() {
		t.Errorf("the child's span is in trace %s, the parent's in %s",
			childOTel.SpanContext().TraceID(), parentOTel.SpanContext().TraceID())
	}

	var childEvent *sentry.Event
	for _, e := range held.transactions() {
		if e.Transaction == "child.work" {
			childEvent = e
		}
	}
	if childEvent == nil {
		t.Fatal("no child.work transaction shipped")
	}
	if got := fmt.Sprintf("%v", traceContext(t, childEvent)["parent_span_id"]); got != parentOTel.SpanContext().SpanID().String() {
		t.Errorf("the child's Sentry span hangs from %s, not from the parent's OpenTelemetry span %s",
			got, parentOTel.SpanContext().SpanID())
	}
}

// 「[Opus 5.5] The traceparent says which span is the cause; whether Sentry keeps the」
// 「[Opus 5.5] child is Sentry's own decision, carried in the baggage. A recorded」
// 「[Opus 5.5] OpenTelemetry span is sampled whatever Sentry decided, so its flag is not」
// 「[Opus 5.5] that decision: a parent Sentry dropped has a child Sentry drops.」
func TestAChildOfADroppedJobIsDroppedWithOTelRecording(t *testing.T) {
	held := &heldTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:              "https://tim@example.com/1",
		Transport:        held,
		EnableTracing:    true,
		TracesSampleRate: 0,
	})
	if err != nil {
		t.Fatalf("could not build a Sentry client: %v", err)
	}
	previous := sentry.CurrentHub().Client()
	sentry.CurrentHub().BindClient(client)
	t.Cleanup(func() { sentry.CurrentHub().BindClient(previous) })
	spans := recording(t)

	pool := poolWith(t, &countingHandler{name: "unused"})
	pool.Registry().Register(&enqueueingHandler{name: "parent.work", queue: pool.queue, child: "child.work"})
	pool.Registry().Register(&countingHandler{name: "child.work"})
	enqueue(t, pool, "parent.work")

	if err := pool.processNextJob(); err != nil {
		t.Fatalf("parent execution: %v", err)
	}
	if err := pool.processNextJob(); err != nil {
		t.Fatalf("child execution: %v", err)
	}

	if len(spans.Ended()) != 2 {
		t.Fatalf("recorded %d OpenTelemetry spans, want the parent's and the child's", len(spans.Ended()))
	}
	for _, e := range held.transactions() {
		t.Errorf("Sentry dropped the parent and shipped %q", e.Transaction)
	}
}
