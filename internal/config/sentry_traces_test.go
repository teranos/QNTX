package config

import (
	"strings"
	"testing"
)

// withSentry builds the least config that passes everything except [sentry], so
// a failure below is about the sample rate and nothing else.
func withSentry(sentry SentryConfig) *Config {
	cfg := &Config{}
	cfg.Storage.Backend = "sqlite"
	cfg.Storage.Sqlite.BoundedStorage.ActorContextLimit = 32
	cfg.Storage.Sqlite.BoundedStorage.ActorContextsLimit = 64
	cfg.Storage.Sqlite.BoundedStorage.EntityActorsLimit = 64
	cfg.Sentry = sentry
	return cfg
}

// shipping is a [sentry] section that is on, so the rate is the only thing left
// to refuse.
func shipping(rate float64) SentryConfig {
	return SentryConfig{
		DSN:              "https://key@o0.ingest.sentry.io/1",
		MinLevel:         "info",
		TracesSampleRate: rate,
	}
}

// The rate is a share, and the SDK reads it as one. A number outside 0..1 is an
// operator who meant a count, and finding that out means looking at an empty
// Trace Explorer and not knowing why.
func TestASampleRateIsAShare(t *testing.T) {
	cases := []struct {
		name string
		rate float64
	}{
		{name: "negative", rate: -0.1},
		{name: "a count, not a share", rate: 100},
		{name: "just over", rate: 1.5},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := withSentry(shipping(c.rate)).Validate()
			if err == nil {
				t.Fatalf("Validate accepted traces_sample_rate = %v", c.rate)
			}
			if !strings.Contains(err.Error(), "traces_sample_rate") {
				t.Errorf("Validate said %q without naming the field", err)
			}
		})
	}
}

func TestTheEndsOfTheRangeAreBothValid(t *testing.T) {
	for _, rate := range []float64{0, 0.25, 1} {
		if err := withSentry(shipping(rate)).Validate(); err != nil {
			t.Errorf("Validate refused traces_sample_rate = %v: %v", rate, err)
		}
	}
}

// Zero means zero: no traces. It is also the default, so a node that has said
// nothing about tracing ships nothing and is not refused for it.
func TestOmittingTheRateShipsNoTraces(t *testing.T) {
	cfg := withSentry(SentryConfig{
		DSN:      "https://key@o0.ingest.sentry.io/1",
		MinLevel: "info",
	})
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate refused a [sentry] section with no traces_sample_rate: %v", err)
	}
	if cfg.Sentry.TracesSampleRate != 0 {
		t.Errorf("an unset traces_sample_rate is %v, not 0", cfg.Sentry.TracesSampleRate)
	}
}

// Without a DSN nothing ships at all, so the rate is never read and never
// refused — the same reading every other field in this section gets.
func TestNoDSNMeansTheRateIsNotRead(t *testing.T) {
	err := withSentry(SentryConfig{TracesSampleRate: 42}).Validate()
	if err != nil {
		t.Fatalf("Validate read traces_sample_rate with no DSN set: %v", err)
	}
}
