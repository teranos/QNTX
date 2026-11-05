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

	"github.com/teranos/QNTX/internal/measure"
	errors "github.com/teranos/sacred-error"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// 「[Fable 5.1] The two variables that each name where spans go, the first over the second.」
const (
	TracesEndpoint = "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"
	Endpoint       = "OTEL_EXPORTER_OTLP_ENDPOINT"
	// 「[Opus 5.5] MetricsEndpoint names where the node's numbers go, over Endpoint.」
	MetricsEndpoint = "OTEL_EXPORTER_OTLP_METRICS_ENDPOINT"
)

// 「[Fable 5.1] flushWithin is how long the process waits for the last batch when it ends.」
const flushWithin = 5 * time.Second

var (
	mu       sync.Mutex
	provider *sdktrace.TracerProvider
	meters   *sdkmetric.MeterProvider
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
	var unexported error
	if said {
		exporter, err := otlptracehttp.New(ctx)
		if err != nil && len(also) == 0 {
			return errors.Wrapf(err, "failed to start the OTLP trace exporter (endpoint=%q)", endpoint)
		}
		if err != nil {
			// The other processors still start: an endpoint that fails is no reason Sentry gets no spans.
			unexported = errors.Wrapf(err, "failed to start the OTLP trace exporter (endpoint=%q); spans go to the other processors alone", endpoint)
		} else {
			options = append(options, sdktrace.WithBatcher(exporter))
		}
	}
	for _, processor := range also {
		options = append(options, sdktrace.WithSpanProcessor(processor))
	}
	// 「[Fable 5.1] The last value of a key wins, so what the environment says of the」
	// 「[Fable 5.1] service stands over what is written here.」
	described, err := describe(ctx, release)
	if err != nil {
		return errors.Wrapf(err, "failed to describe the node as an OpenTelemetry resource (endpoint=%q, release=%q)", endpoint, release)
	}

	provider = sdktrace.NewTracerProvider(append(options, sdktrace.WithResource(described))...)
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return unexported
}

// 「[Fable 5.1] Running reports whether spans are leaving the node.」
func Running() bool {
	mu.Lock()
	defer mu.Unlock()
	return provider != nil
}

// 「[Opus 5.5] MetricsSaid is the endpoint the environment names for the node's numbers, and whether it names one.」
func MetricsSaid() (string, bool) {
	for _, name := range []string{MetricsEndpoint, Endpoint} {
		if endpoint := os.Getenv(name); endpoint != "" {
			return endpoint, true
		}
	}
	return "", false
}

// 「[Opus 5.5] StartMetrics sends the numbers internal/measure takes over OTLP/HTTP, protobuf, to the」
// 「[Opus 5.5] endpoint said, under the names they have, beside Sentry. The exporter reads the rest of」
// 「[Opus 5.5] OpenTelemetry's own variables itself: OTEL_EXPORTER_OTLP_METRICS_HEADERS or」
// 「[Opus 5.5] OTEL_EXPORTER_OTLP_HEADERS for what the receiver asks to be let in, and」
// 「[Opus 5.5] OTEL_METRIC_EXPORT_INTERVAL for how often. No endpoint said starts nothing.」
func StartMetrics(ctx context.Context, release string) error {
	endpoint, said := MetricsSaid()
	if !said {
		return nil
	}

	mu.Lock()
	defer mu.Unlock()
	if meters != nil {
		return errors.New("otlp.StartMetrics called twice: the second provider would replace the first and leave its numbers unsent")
	}

	exporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return errors.Wrapf(err, "failed to start the OTLP metric exporter (endpoint=%q)", endpoint)
	}
	described, err := describe(ctx, release)
	if err != nil {
		return errors.Wrapf(err, "failed to describe the node as an OpenTelemetry resource (endpoint=%q, release=%q)", endpoint, release)
	}
	meters = sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
		sdkmetric.WithResource(described),
	)
	measure.ExportTo(meters.Meter("github.com/teranos/QNTX/internal/measure"))
	return nil
}

// 「[Opus 5.5] MetricsRunning reports whether the node's numbers are leaving over OTLP.」
func MetricsRunning() bool {
	mu.Lock()
	defer mu.Unlock()
	return meters != nil
}

// 「[Fable 5.1] Flush sends what the batch still holds and stops the provider. The」
// 「[Fable 5.1] exporter batches, so a process that ends without this drops the spans」
// 「[Fable 5.1] of its last seconds. Safe to call when nothing started.」
func Flush() error {
	mu.Lock()
	defer mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), flushWithin)
	defer cancel()
	var spans, numbers error
	if provider != nil {
		spans = provider.Shutdown(ctx)
		provider = nil
	}
	// 「[Opus 5.5] The node's last numbers are read and sent before the meter stops, and nothing is measured into it after.」
	if meters != nil {
		measure.ExportTo(nil)
		numbers = meters.Shutdown(ctx)
		meters = nil
	}
	if spans != nil {
		return errors.WithSecondaryError(errors.Wrapf(spans, "the last OTLP batch did not leave within %s", flushWithin), numbers)
	}
	if numbers != nil {
		return errors.Wrapf(numbers, "the node's last numbers did not leave over OTLP within %s", flushWithin)
	}
	return nil
}

// 「[Opus 5.5] describe is the node as an OpenTelemetry resource, the same for its spans and its numbers.」
func describe(ctx context.Context, release string) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", "qntx"),
			attribute.String("service.version", release),
		),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
	)
}
