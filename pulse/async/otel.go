package async

import (
	"context"

	"github.com/getsentry/sentry-go"
	"github.com/teranos/QNTX/internal/otlp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// 「[Fable 5.1] A job's execution as an OpenTelemetry span, beside the Sentry one」
// 「[Fable 5.1] startJobSpan opens. A schedule's run is a job the ticker enqueues, so」
// 「[Fable 5.1] it is covered here too.」
//
// 「[Fable 5.1] The OpenTelemetry span is a child of the Sentry span and so in its」
// 「[Fable 5.1] trace: the trace id a job inherits across the queue is the one both」
// 「[Fable 5.1] carry, and a trace is one trace wherever it is looked at. The row」
// 「[Fable 5.1] still holds Sentry's headers (migration 062), so between a job and」
// 「[Fable 5.1] its child the parent named is a Sentry span.」

// 「[Fable 5.1] tracerName is the instrumentation scope the job spans are opened under.」
const tracerName = "github.com/teranos/QNTX/pulse/async"

// 「[Fable 5.1] startOTelJobSpan opens the span and returns the context the handler」
// 「[Fable 5.1] runs under, which holds both spans. With no OTLP endpoint said it」
// 「[Fable 5.1] returns the Sentry span's context as it is, and nothing changes.」
//
// 「[Fable 5.1] The name and the attributes are OpenTelemetry's for a message being」
// 「[Fable 5.1] processed, where it has one; job.* are ours, the same keys the」
// 「[Fable 5.1] Sentry span carries.」
func startOTelJobSpan(span *sentry.Span, job *Job) context.Context {
	if !otlp.Running() {
		return span.Context()
	}
	// 「[Fable 5.1] Sampled is this side's own decision to record: Sentry's rate」
	// 「[Fable 5.1] does not decide what leaves over OTLP.」
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID(span.TraceID),
		SpanID:     trace.SpanID(span.SpanID),
		TraceFlags: trace.FlagsSampled,
	})
	attrs := []attribute.KeyValue{
		attribute.String("messaging.system", "pulse"),
		attribute.String("messaging.operation.type", "process"),
		attribute.String("messaging.destination.name", job.HandlerName),
		attribute.String("messaging.message.id", job.ID),
		attribute.String("job.source", job.Source),
		attribute.Int("job.retry_count", job.RetryCount),
		attribute.Int("job.progress_total", job.Progress.Total),
	}
	if job.ParentJobID != "" {
		attrs = append(attrs, attribute.String("job.parent_job_id", job.ParentJobID))
	}
	if job.PluginVersion != "" {
		attrs = append(attrs, attribute.String("job.plugin_version", job.PluginVersion))
	}
	ctx, _ := otel.Tracer(tracerName).Start(
		trace.ContextWithSpanContext(span.Context(), parent),
		"process "+job.HandlerName,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attrs...),
	)
	return ctx
}

// 「[Fable 5.1] recordingOTel reports whether the context holds an OpenTelemetry span」
// 「[Fable 5.1] that is being recorded.」
func recordingOTel(ctx context.Context) bool {
	return trace.SpanFromContext(ctx).IsRecording()
}

// 「[Fable 5.1] finishOTelJobSpan closes the span with how the execution ended, read」
// 「[Fable 5.1] off the Sentry span markSpan wrote it to, so the two cannot say」
// 「[Fable 5.1] different things. Only a failed handler is an error; a job re-queued」
// 「[Fable 5.1] is left unset and named by pulse.outcome.」
func finishOTelJobSpan(ctx context.Context, span *sentry.Span, job *Job) {
	speaking := trace.SpanFromContext(ctx)
	speaking.SetAttributes(attribute.Int("job.progress_current", job.Progress.Current))
	if outcome, ok := span.Data["pulse.outcome"].(string); ok {
		speaking.SetAttributes(attribute.String("pulse.outcome", outcome))
	}
	switch span.Status {
	case sentry.SpanStatusOK:
		speaking.SetStatus(codes.Ok, "")
	case sentry.SpanStatusInternalError:
		failed, _ := span.Data["error"].(string)
		speaking.SetStatus(codes.Error, failed)
	}
	speaking.End()
}
