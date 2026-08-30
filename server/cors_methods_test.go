package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
)

// allowedMethods asks the preflight what the browser is told it may send.
func allowedMethods(t *testing.T) string {
	t.Helper()
	s := &QNTXServer{logger: zap.NewNop().Sugar()}
	handler := s.corsMiddleware(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodOptions, "/auth/tokens/x/scope", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	return rec.Header().Get("Access-Control-Allow-Methods")
}

// A method this API serves and the preflight omits reaches a browser as a
// network error naming nothing, and curl never asks, so nothing else finds it.
func TestThePreflightNamesEveryMethodTheAPIServes(t *testing.T) {
	said := allowedMethods(t)
	for _, method := range []string{
		http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
	} {
		if !strings.Contains(said, method) {
			t.Errorf("the preflight does not allow %s: %q", method, said)
		}
	}
}
