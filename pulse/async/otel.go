package async

import (
	"context"
	"fmt"

	"github.com/getsentry/sentry-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// 「[Fable 5.1] A job's execution as an OpenTelemetry span, beside the Sentry one」
// 「[Fable 5.1] startJobSpan opens. A schedule's run is a job the ticker enqueues, so」
// 「[Fable 5.1] it is covered here too.」
//
// 「[Fable 5.1] The OpenTelemetry span is a child of the Sentry span and so in its」
// 「[Fable 5.1] trace: the trace id a job inherits across the queue is the one both」
// 「[Fable 5.1] carry, and a trace is one trace wherever it is looked at.」
// 「[Opus 5.5] The row holds W3C trace context, and the span it names is the」
// 「[Opus 5.5] innermost one running: the OpenTelemetry span when one is recorded,」
// 「[Opus 5.5] the Sentry span when not.」

// 「[Fable 5.1] tracerName is the instrumentation scope the job spans are opened under.」
const tracerName = "github.com/teranos/QNTX/pulse/async"

// 「[Fable 5.1] startOTelJobSpan opens the span and returns the context the handler」
// 「[Fable 5.1] runs under, which holds both spans. With no OTLP endpoint said it」
// 「[Fable 5.1] returns the Sentry span's context as it is, and nothing changes.」
// 「[Opus 5.5] No endpoint said leaves OpenTelemetry's no-op provider in place, whose」
// 「[Opus 5.5] span records nothing; that is the test, so a provider set another way」
// 「[Opus 5.5] is used too.」
//
// 「[Fable 5.1] The name and the attributes are OpenTelemetry's for a message being」
// 「[Fable 5.1] processed, where it has one; job.* are ours, the same keys the」
// 「[Fable 5.1] Sentry span carries.」
func startOTelJobSpan(span *sentry.Span, job *Job) context.Context {
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
	ctx, opened := otel.Tracer(tracerName).Start(
		trace.ContextWithSpanContext(span.Context(), parent),
		"process "+job.HandlerName,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attrs...),
	)
	if !opened.IsRecording() {
		return span.Context()
	}
	return ctx
}

// 「[Opus 5.5] traceparentOf is the W3C traceparent of the innermost span in ctx, and」
// 「[Opus 5.5] the baggage that goes with it. Empty is no span.」
//
// 「[Opus 5.5] The baggage is the Sentry span's dynamic sampling context, which Sentry」
// 「[Opus 5.5] writes as a W3C baggage header. No span the node opens carries」
// 「[Opus 5.5] tracestate: the OpenTelemetry job span's parent is built without one.」
func traceparentOf(ctx context.Context) (traceparent, bag string) {
	sentrySpan := sentry.SpanFromContext(ctx)
	if sentrySpan != nil {
		bag = sentrySpan.ToBaggage()
	}
	if trace.SpanFromContext(ctx).IsRecording() {
		carrier := propagation.MapCarrier{}
		propagation.TraceContext{}.Inject(ctx, carrier)
		return carrier.Get("traceparent"), bag
	}
	if sentrySpan != nil {
		return sentrySpan.ToTraceparent(), bag
	}
	return "", ""
}

// 「[Opus 5.5] continueFrom is the Sentry span option that continues the traceparent」
// 「[Opus 5.5] a row holds. Sentry's SDK reads only its own header, so the parsed」
// 「[Opus 5.5] context is handed to it in that form here and nowhere else. A value」
// 「[Opus 5.5] that is not a traceparent, a row written before the columns held W3C」
// 「[Opus 5.5] trace context among them, is no cause: the span heads its own trace.」
//
// 「[Opus 5.5] Whether Sentry keeps the span is Sentry's decision, which its baggage」
// 「[Opus 5.5] carries as sentry-sampled. The traceparent's flag is not it: a recorded」
// 「[Opus 5.5] OpenTelemetry job span is sampled whatever Sentry decided. No」
// 「[Opus 5.5] sentry-sampled leaves the decision to the span's own sample rate.」
func continueFrom(traceparent, bag string) sentry.SpanOption {
	cause := trace.SpanContextFromContext(propagation.TraceContext{}.Extract(
		context.Background(), propagation.MapCarrier{"traceparent": traceparent}))
	if !cause.IsValid() {
		return sentry.ContinueFromHeaders("", "")
	}
	header := fmt.Sprintf("%s-%s", cause.TraceID(), cause.SpanID())
	if members, err := baggage.Parse(bag); err == nil {
		switch members.Member("sentry-sampled").Value() {
		case "true":
			header += "-1"
		case "false":
			header += "-0"
		}
	}
	return sentry.ContinueFromHeaders(header, bag)
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
