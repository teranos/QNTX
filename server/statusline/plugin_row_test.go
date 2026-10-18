package statusline

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/plugin"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/server/auth"
)

// A route says how often it was called and how long it took, and a 5xx is the
// one thing that makes it unwell: a 4xx is a caller asking wrong.
func TestARouteSaysItsCallsAndWhatItAnsweredBadly(t *testing.T) {
	quiet := routeItem(grpcplugin.RouteTraffic{Route: "POST /me/draft", Calls: 4, Took: 40 * time.Millisecond})
	if quiet.Name != "POST /me/draft" || quiet.Note != "4 10ms" || quiet.Symbol != SymbolWell {
		t.Fatalf("got %+v", quiet)
	}

	refused := routeItem(grpcplugin.RouteTraffic{Route: "GET /coverage", Calls: 2, Refused: 2, Took: 2 * time.Millisecond})
	if refused.Note != "2 1ms, 2 4xx" || refused.Symbol != SymbolWell {
		t.Fatalf("got %+v", refused)
	}

	broke := routeItem(grpcplugin.RouteTraffic{Route: "POST /me/claim", Calls: 3, Broke: 1, Took: 3 * time.Millisecond})
	if broke.Note != "3 1ms, 1 5xx" || broke.Symbol != SymbolUnwell {
		t.Fatalf("got %+v", broke)
	}
}

// A plugin the node does not hold is said, not answered with the node's row.
func TestAPluginRowForNoSuchPluginIsA404(t *testing.T) {
	var h *StatusLineHandler
	req := httptest.NewRequest(http.MethodGet, "/am/statusline?format=json&plugin=nobody", nil)
	req = req.WithContext(auth.WithAdmission(req.Context(), auth.Admitted(auth.LevelRoot)))
	rec := httptest.NewRecorder()
	h.HandleStatusLine(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("got %d: %s", rec.Code, rec.Body.String())
	}
}

// aPlugin is a plugin that is only a name and a version.
type aPlugin struct{ name, version string }

func (p aPlugin) Metadata() plugin.Metadata {
	return plugin.Metadata{Name: p.name, Version: p.version, QNTXVersion: ">=0.0.0"}
}
func (aPlugin) Initialize(context.Context, plugin.ServiceRegistry) error { return nil }
func (aPlugin) Shutdown(context.Context) error                           { return nil }
func (aPlugin) RegisterHTTP(*http.ServeMux) error                        { return nil }
func (aPlugin) RegisterWebSocket() (map[string]plugin.WebSocketHandler, error) {
	return nil, nil
}
func (aPlugin) Health(context.Context) plugin.HealthStatus { return plugin.HealthStatus{} }

// The healthy plugins share one slot and take turns in it; one that is unwell
// keeps its own, and so does one with a failing handler, however well it is.
func TestHealthyPluginsShareOneSlot(t *testing.T) {
	reg := plugin.NewRegistry("1.0.0", zap.NewNop().Sugar())
	for _, p := range []aPlugin{{"a", "1.0.0"}, {"b", "2.0.0"}, {"c", "3.0.0"}, {"d", "4.0.0"}} {
		require.NoError(t, reg.Register(p))
		reg.MarkReady(p.name)
	}
	health := map[string]plugin.HealthStatus{
		"a": {Healthy: true}, "b": {Healthy: true}, "c": {Healthy: false}, "d": {Healthy: true},
	}
	log := NewHandlerFailureLog()
	log.Record(HandlerFailure{Handler: "d/harvest", Error: "no water", AtMs: time.Now().UnixMilli()})
	h := NewStatusLineHandler(reg, zap.NewNop().Sugar(),
		func() map[string]plugin.HealthStatus { return health },
		func() storage.Watchers { return nil },
		func() *HandlerFailureLog { return log }, fullNode{})

	req := httptest.NewRequest(http.MethodGet, "/am/statusline?format=json", nil)
	req = req.WithContext(auth.WithAdmission(req.Context(), auth.Admitted(auth.LevelSuper)))
	rec := httptest.NewRecorder()
	h.HandleStatusLine(rec, req)
	var row StatusLineResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &row))

	named := map[string]int{}
	for _, it := range row.Items {
		named[it.Name]++
	}
	assert.Equal(t, 1, named["c"], "an unwell plugin is pinned by name: %+v", row.Items)
	assert.Equal(t, 1, named["d/harvest"], "a plugin with a failing handler keeps its slot, drawn as the failure: %+v", row.Items)
	assert.Equal(t, 1, named["a"]+named["b"], "the healthy share one slot: %+v", row.Items)
}
