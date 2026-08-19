package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/teranos/QNTX/plugin"
	"go.uber.org/zap"
)

// A handler that probes on every request costs plugins × requests in gRPC calls
// and holds the registry's read lock across them. It reads a snapshot now.
func TestTheHandlerReadsTheProbeRatherThanMakingOne(t *testing.T) {
	probes := 0
	s := &QNTXServer{logger: zap.NewNop().Sugar()}
	h := NewPluginHandler(plugin.NewRegistry("test", zap.NewNop().Sugar()), zap.NewNop().Sugar(),
		func() (map[string]plugin.HealthStatus, time.Time, string) {
			probes++
			return map[string]plugin.HealthStatus{}, time.Now(), ""
		})
	_ = s

	for i := 0; i < 5; i++ {
		h.HandlePlugins(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/plugins", nil))
	}

	// Five requests read the snapshot five times and reach no plugin. What is
	// being asserted is that reading is all a request does.
	if probes != 5 {
		t.Errorf("snapshot reads = %d, want 5", probes)
	}
}

// The whole reason for the timestamp: a snapshot with no age on it cannot be
// told from a current answer, which is how /health said ok through a dead node.
func TestTheAnswerSaysWhenItWasProbed(t *testing.T) {
	probedAt := time.Now().Add(-90 * time.Second)
	h := NewPluginHandler(plugin.NewRegistry("test", zap.NewNop().Sugar()), zap.NewNop().Sugar(),
		func() (map[string]plugin.HealthStatus, time.Time, string) {
			return map[string]plugin.HealthStatus{}, probedAt, ""
		})

	rec := httptest.NewRecorder()
	h.HandlePlugins(rec, httptest.NewRequest(http.MethodGet, "/api/plugins", nil))

	var said map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &said); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if said["health_probed_at"] == nil {
		t.Error("the answer does not say when it was probed")
	}
	age, ok := said["health_age_ms"].(float64)
	if !ok {
		t.Fatalf("health_age_ms missing or not a number: %v", said["health_age_ms"])
	}
	if age < 89_000 {
		t.Errorf("health_age_ms = %v, want at least 89000", age)
	}
}

// An empty result set with nothing said reads as "no plugins". The handler
// carries the reason through so the two are different answers.
func TestAnUnansweredProbeIsSaidRatherThanHidden(t *testing.T) {
	h := NewPluginHandler(plugin.NewRegistry("test", zap.NewNop().Sugar()), zap.NewNop().Sugar(),
		func() (map[string]plugin.HealthStatus, time.Time, string) {
			return nil, time.Time{}, "no probe has completed yet"
		})

	rec := httptest.NewRecorder()
	h.HandlePlugins(rec, httptest.NewRequest(http.MethodGet, "/api/plugins", nil))

	var said map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &said); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if said["health_probe_failure"] != "no probe has completed yet" {
		t.Errorf("health_probe_failure = %v, want the reason stated", said["health_probe_failure"])
	}
}

// Before the first probe completes there is no answer, and saying so beats
// publishing an empty one that reads as "no plugins".
func TestNoProbeYetIsNotAnEmptyAnswer(t *testing.T) {
	s := &QNTXServer{logger: zap.NewNop().Sugar()}

	results, at, failure := s.pluginHealth()
	if results != nil {
		t.Errorf("results = %v, want nil before any probe", results)
	}
	if !at.IsZero() {
		t.Errorf("at = %v, want zero before any probe", at)
	}
	if failure == "" {
		t.Error("no probe has completed and nothing says so")
	}
}
