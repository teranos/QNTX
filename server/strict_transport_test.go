package server

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// A2A §13.4: a response over https carries HSTS, and one over plain http does
// not.
func TestAResponseOverHTTPSCarriesStrictTransport(t *testing.T) {
	served := strictTransport(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))

	forwarded := httptest.NewRequest(http.MethodGet, "/a2a/tasks", nil)
	forwarded.Header.Set("X-Forwarded-Proto", "https")
	direct := httptest.NewRequest(http.MethodGet, "/api/types", nil)
	direct.TLS = &tls.ConnectionState{}
	for name, r := range map[string]*http.Request{"forwarded": forwarded, "direct": direct} {
		w := httptest.NewRecorder()
		served.ServeHTTP(w, r)
		assert.Equal(t, "max-age=31536000", w.Header().Get("Strict-Transport-Security"), name)
	}

	w := httptest.NewRecorder()
	served.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/a2a/tasks", nil))
	assert.Empty(t, w.Header().Get("Strict-Transport-Security"), "plain http")
}
