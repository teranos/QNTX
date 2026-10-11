package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/teranos/QNTX/plugin"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

// A handler that probes on every request costs plugins × requests in gRPC calls
// and holds the registry's read lock across them. It reads a snapshot now.
func TestTheHandlerReadsTheProbeRatherThanMakingOne(t *testing.T) {
	probes := 0
	s := &QNTXServer{pluginRegistry: plugin.GetDefaultRegistry(), logger: zap.NewNop().Sugar()}
	h := NewPluginHandler(plugin.NewRegistry("test", zap.NewNop().Sugar()), zap.NewNop().Sugar(),
		func() pluginProbe {
			probes++
			return probed{results: map[string]plugin.HealthStatus{}, at: time.Now()}
		}, noSigils, noRecords)
	_ = s

	for i := 0; i < 5; i++ {
		h.list()
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
		func() pluginProbe {
			return probed{results: map[string]plugin.HealthStatus{}, at: probedAt}
		}, noSigils, noRecords)

	said := answeredAsJSON(t, h.list())
	health, ok := said["health"].(map[string]any)
	if !ok {
		t.Fatalf("the answer does not say when it was probed: %v", said["health"])
	}
	if health["probed_at"] == "" {
		t.Error("the answer does not say when it was probed")
	}
	age, ok := health["age_ms"].(float64)
	if !ok {
		t.Fatalf("age_ms missing or not a number: %v", health["age_ms"])
	}
	if age < 89_000 {
		t.Errorf("age_ms = %v, want at least 89000", age)
	}
	holds(t, bareNode().pluginsSignum(), "list", h.list())
	routes, err := h.routes()
	if err != nil {
		t.Fatalf("routes: %v", err)
	}
	holds(t, bareNode().pluginsSignum(), "routes", routes)
	holds(t, bareNode().pluginsSignum(), "elements", h.elements(context.Background()))
	// A width a plugin names is said; one it omits is left out.
	holds(t, bareNode().pluginsSignum(), "elements", &protocol.PluginElements{Elements: []*protocol.PluginElement{
		{Plugin: "pty-element", Symbol: "⌨", DefaultWidth: proto.Int32(800), DefaultHeight: proto.Int32(600)},
		{Plugin: "hello-world", Symbol: "👋"},
	}})
}

// An empty result set with nothing said reads as "no plugins". The handler
// carries the reason through so the two are different answers.
func TestAnUnansweredProbeIsSaidRatherThanHidden(t *testing.T) {
	h := NewPluginHandler(plugin.NewRegistry("test", zap.NewNop().Sugar()), zap.NewNop().Sugar(),
		func() pluginProbe {
			return notProbedYet{why: "no probe has completed yet"}
		}, noSigils, noRecords)

	said := answeredAsJSON(t, h.list())
	if said["health_probe_failure"] != "no probe has completed yet" {
		t.Errorf("health_probe_failure = %v, want the reason stated", said["health_probe_failure"])
	}
}

// noSigils is a handler's node serving no plugin's sigils.
func noSigils(string) ([]*protocol.SigilRow, []string) { return nil, nil }

// noRecords is a handler's node that knows no plugin.
func noRecords() ([]grpcplugin.PluginRecord, error) { return nil, nil }

// answeredAsJSON is an answer as the surfaces carry it: marshalled.
func answeredAsJSON(t *testing.T, answer any) map[string]any {
	t.Helper()
	body, err := answerJSON(answer)
	if err != nil {
		t.Fatalf("answer does not marshal: %v", err)
	}
	var said map[string]any
	if err := json.Unmarshal(body, &said); err != nil {
		t.Fatalf("answer is not a JSON object: %v", err)
	}
	return said
}

// Before the first probe completes there is no answer, and saying so beats
// publishing an empty one that reads as "no plugins".
func TestNoProbeYetIsNotAnEmptyAnswer(t *testing.T) {
	s := &QNTXServer{pluginRegistry: plugin.GetDefaultRegistry(), logger: zap.NewNop().Sugar()}
	// As NewQNTXServer holds it before the refresher's first probe.
	s.pluginHealthCache.Store(notProbed())

	held, notYet := s.pluginHealth().(notProbedYet)
	if !notYet {
		t.Fatalf("before any probe the node holds %#v", s.pluginHealth())
	}
	if held.why == "" {
		t.Error("no probe has completed and nothing says so")
	}
}
