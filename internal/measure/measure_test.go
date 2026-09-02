package measure

import (
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
)

// bind starts a Sentry client against a transport the test can read, and takes
// it away again afterwards so the next test in this package starts clean.
func bind(t *testing.T) *sentry.MockTransport {
	t.Helper()

	transport := &sentry.MockTransport{}
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:       "https://key@example.invalid/1",
		Transport: transport,
	}); err != nil {
		t.Fatalf("sentry.Init returned %v", err)
	}
	Start()

	t.Cleanup(func() {
		sentry.CurrentHub().BindClient(nil)
		meter.Store(nil)
	})
	return transport
}

// A node with no DSN pays nothing and crashes on nothing. Every call below runs
// against a meter that was never started.
func TestCallsWithoutStartDoNothing(t *testing.T) {
	meter.Store(nil)

	Count(AttestationsWritten, 1)
	Gauge(QueueDepth, 4)
	Took(QueryTook, 12*time.Millisecond)
	Sized(QueryReturned, 100)
	Count(Refused, 1, String(AttrOutcome, "no-session"))
}

// Start with no Sentry client behind it leaves a meter that discards. This is
// the shape a node runs in when am.toml names no DSN.
func TestStartWithoutAClientDiscards(t *testing.T) {
	sentry.CurrentHub().BindClient(nil)
	Start()
	t.Cleanup(func() { meter.Store(nil) })

	Count(AttestationsWritten, 1)
	Gauge(QueueDepth, 4)
	Took(QueryTook, 12*time.Millisecond)
	Sized(QueryReturned, 100)
}

// Nothing is replaced on the way out. A call site that puts a secret in a
// dimension is the fault, and hiding it here would leave that call site standing
// while the value is still in the console, the file and journald.
func TestEveryDimensionTravelsAsItWasWritten(t *testing.T) {
	for _, key := range []string{"access_token", "client_secret", "cookie"} {
		attrs := permitted([]Attr{String(key, "the-value-itself")})

		if len(attrs) != 1 {
			t.Fatalf("permitted dropped %q", key)
		}
		if attrs[0].Key != key {
			t.Errorf("permitted renamed %q to %q", key, attrs[0].Key)
		}
		if got := attrs[0].Value.AsString(); got != "the-value-itself" {
			t.Errorf("dimension %q carries %q, want what was written", key, got)
		}
	}
}

// A dimension that names nothing sensitive travels as it was written.
func TestOrdinaryDimensionsTravelWhole(t *testing.T) {
	attrs := permitted([]Attr{String(AttrLevel, "ATTESTOR"), String(AttrOutcome, "no-session")})

	if len(attrs) != 2 {
		t.Fatalf("permitted returned %d dimensions, want 2", len(attrs))
	}
	if attrs[0].Value.AsString() != "ATTESTOR" || attrs[1].Value.AsString() != "no-session" {
		t.Errorf("permitted changed a dimension it had no reason to: %v", attrs)
	}
}

// permitted must not allocate a slice for the common case of no dimensions.
func TestNoDimensionsIsNil(t *testing.T) {
	if attrs := permitted(nil); attrs != nil {
		t.Errorf("permitted(nil) = %v, want nil", attrs)
	}
}

// The end of the path: a metric emitted through the real SDK reaches the
// transport. Metrics are batched, so the flush is what makes them arrive.
func TestMetricsReachTheTransport(t *testing.T) {
	transport := bind(t)

	Count(AttestationsWritten, 1)
	Gauge(QueueDepth, 7)
	Took(QueryTook, 25*time.Millisecond)
	Sized(QueryReturned, 42)

	sentry.Flush(2 * time.Second)

	if len(transport.Events()) == 0 {
		t.Fatal("no envelope reached the transport, so nothing about the metric path is proven")
	}
}
