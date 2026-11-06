// 「[Fable 5.1] Package otlp is the node speaking OpenTelemetry: the spans it opens leave」
// 「[Fable 5.1] over OTLP/HTTP, protobuf, to where the environment says.」
//
// 「[Fable 5.1] Where is said in OpenTelemetry's own variables, which the exporter」
// 「[Fable 5.1] reads itself: OTEL_EXPORTER_OTLP_TRACES_ENDPOINT or」
// 「[Fable 5.1] OTEL_EXPORTER_OTLP_ENDPOINT, and OTEL_EXPORTER_OTLP_TRACES_HEADERS or」
// 「[Fable 5.1] OTEL_EXPORTER_OTLP_HEADERS for what the receiver asks to be let in.」
// 「[Fable 5.1] Neither endpoint said is the off switch: nothing starts, and a span a」
// 「[Fable 5.1] call site opens goes to OpenTelemetry's no-op tracer.」
//
// 「[Fable 5.1] What is spoken is pinned: server/parity/otlp_v1.11.0_790608c, the」
// 「[Fable 5.1] commit go.opentelemetry.io/proto/otlp is generated from.」
package otlp

import (
	"context"
	"os"
	"sync"
	"time"

	errors "github.com/teranos/sacred-error"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// 「[Fable 5.1] The two variables that each name where spans go, the first over the second.」
const (
	TracesEndpoint = "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"
	Endpoint       = "OTEL_EXPORTER_OTLP_ENDPOINT"
)

// 「[Fable 5.1] flushWithin is how long the process waits for the last batch when it ends.」
const flushWithin = 5 * time.Second

var (
	mu       sync.Mutex
	provider *sdktrace.TracerProvider
)

// 「[Fable 5.1] Said is the endpoint the environment names for spans, and whether it names one.」
func Said() (string, bool) {
	for _, name := range []string{TracesEndpoint, Endpoint} {
		if endpoint := os.Getenv(name); endpoint != "" {
			return endpoint, true
		}
	}
	return "", false
}

// 「[Fable 5.1] Start makes the node's tracer provider the one every otel.Tracer call」
// 「[Fable 5.1] reaches, exporting in batches to the endpoint said, and W3C traceparent」
// 「[Fable 5.1] the way a trace is carried in and out. No endpoint said returns nil」
// 「[Fable 5.1] without starting anything.」
//
// 「[Fable 5.1] release is the build, as service.version. service.name is qntx unless」
// 「[Fable 5.1] OTEL_SERVICE_NAME or OTEL_RESOURCE_ATTRIBUTES says another.」
//
// also is where else each span goes, Sentry's span processor among them when
// Sentry traces: given one, the provider starts with no endpoint said.
func Start(ctx context.Context, release string, also ...sdktrace.SpanProcessor) error {
	endpoint, said := Said()
	if !said && len(also) == 0 {
		return nil
	}

	mu.Lock()
	defer mu.Unlock()
	if provider != nil {
		return errors.New("otlp.Start called twice: the second provider would replace the first and leave its batch unsent")
	}

	options := []sdktrace.TracerProviderOption{}
	if said {
		exporter, err := otlptracehttp.New(ctx)
		if err != nil {
			return errors.Wrapf(err, "failed to start the OTLP trace exporter (endpoint=%q)", endpoint)
		}
		options = append(options, sdktrace.WithBatcher(exporter))
	}
	for _, processor := range also {
		options = append(options, sdktrace.WithSpanProcessor(processor))
	}
	// 「[Fable 5.1] The last value of a key wins, so what the environment says of the」
	// 「[Fable 5.1] service stands over what is written here.」
	described, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", "qntx"),
			attribute.String("service.version", release),
		),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return errors.Wrapf(err, "failed to describe the node as an OpenTelemetry resource (endpoint=%q, release=%q)", endpoint, release)
	}

	provider = sdktrace.NewTracerProvider(append(options, sdktrace.WithResource(described))...)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return nil
}

// 「[Fable 5.1] Running reports whether spans are leaving the node.」
func Running() bool {
	mu.Lock()
	defer mu.Unlock()
	return provider != nil
}

// 「[Fable 5.1] Flush sends what the batch still holds and stops the provider. The」
// 「[Fable 5.1] exporter batches, so a process that ends without this drops the spans」
// 「[Fable 5.1] of its last seconds. Safe to call when nothing started.」
func Flush() error {
	mu.Lock()
	defer mu.Unlock()
	if provider == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), flushWithin)
	defer cancel()
	err := provider.Shutdown(ctx)
	provider = nil
	if err != nil {
		return errors.Wrapf(err, "the last OTLP batch did not leave within %s", flushWithin)
	}
	return nil
}
