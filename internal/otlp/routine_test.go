package otlp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// What the rule calls routine never reaches the processor; the rest does.
func TestARoutineSpanNeverReachesTheProcessor(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(notRoutine{
		next:      recorder,
		isRoutine: func(s sdktrace.ReadOnlySpan) bool { return s.Name() == "poll" },
	}))
	tracer := provider.Tracer("test")
	for _, name := range []string{"poll", "request", "poll"} {
		_, span := tracer.Start(context.Background(), name)
		span.End()
	}
	ended := recorder.Ended()
	if assert.Len(t, ended, 1) {
		assert.Equal(t, "request", ended[0].Name())
	}
}

// Spans that go to Sentry's OTLP ingest need not go to Sentry again.
func TestSentrysOwnIngestIsKnownByItsHost(t *testing.T) {
	for _, c := range []struct {
		endpoint string
		sentry   bool
	}{
		{"https://o4511990405464064.ingest.de.sentry.io/api/1/integration/otlp/v1/traces", true},
		{"https://sentry.io/api/1/otlp", true},
		{"https://otel.example.com/v1/traces", false},
		{"https://notsentry.io.example.com/v1", false},
	} {
		endpoint, sentry := c.endpoint, c.sentry
		t.Setenv(TracesEndpoint, endpoint)
		assert.Equal(t, sentry, SaidIsSentry(), endpoint)
	}
}
