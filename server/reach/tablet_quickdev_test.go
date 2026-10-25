//go:build quickdev

package reach

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "right now its just friction without reward"
func TestQuickDevOpensWithoutTheTablet(t *testing.T) {
	served, unnamed, err := Open(map[string]Answering{
		"/health": {Handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }},
	}, plainly(), Runtime{})
	require.NoError(t, err)
	// Unnamed is ROOT's, which every caller is on a node without auth.
	assert.Equal(t, []string{"/health"}, unnamed)

	w := httptest.NewRecorder()
	served.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}
