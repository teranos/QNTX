package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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
