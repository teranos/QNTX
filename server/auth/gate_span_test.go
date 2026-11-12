package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// servedSpan is the one span a request served through otelhttp and the gate
// leaves.
func servedSpan(t *testing.T, gate http.Handler, req *http.Request) (sdktrace.ReadOnlySpan, int) {
	t.Helper()
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	rec := httptest.NewRecorder()
	otelhttp.NewHandler(gate, "served", otelhttp.WithTracerProvider(provider)).ServeHTTP(rec, req)
	spans := recorder.Ended()
	require.Len(t, spans, 1)
	return spans[0], rec.Code
}

func attributeOf(span sdktrace.ReadOnlySpan, key string) (string, bool) {
	for _, kv := range span.Attributes() {
		if kv.Key == attribute.Key(key) {
			return kv.Value.AsString(), true
		}
	}
	return "", false
}

// A request with no credential is refused, and its span says its route and
// why it was refused.
func TestARefusalIsReadInItsSpan(t *testing.T) {
	h := &Handler{users: &memUsers{}, sessions: newSessionStore(1), logger: testLogger()}
	gate := h.Middleware("/mcp", everyLevel, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	req.Header.Set("Accept", "application/json")
	span, code := servedSpan(t, gate, req)

	assert.Equal(t, http.StatusUnauthorized, code)
	assert.Equal(t, "POST /mcp", span.Name())
	why, said := attributeOf(span, "qntx.refused")
	assert.True(t, said)
	assert.Equal(t, "no-session", why)
}

// An admitted request's span says its route and the level it was admitted at.
func TestAnAdmissionIsReadInItsSpan(t *testing.T) {
	sessions := newSessionStore(1)
	token, _ := sessions.create(mastodonAccount, User{})
	h := &Handler{users: &memUsers{}, sessions: sessions, logger: testLogger()}
	h.SetIdentities([]string{mastodonAccount}, nil)
	gate := h.Middleware("/mcp", everyLevel, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	span, code := servedSpan(t, gate, req)

	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "GET /mcp", span.Name())
	level, said := attributeOf(span, "qntx.level")
	assert.True(t, said)
	assert.Equal(t, "ROOT", level)
	_, refused := attributeOf(span, "qntx.refused")
	assert.False(t, refused)
}
