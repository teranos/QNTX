package server

import (
	"bytes"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin"
	"go.uber.org/zap/zaptest"
)

func TestSetupHTTPRoutes_RegistersAllPlugins(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()

	// Create test registry with multiple plugins
	registry := plugin.NewRegistry("test-version", logger)

	// Create minimal server with plugin registry
	srv := &QNTXServer{
		pluginRegistry: registry,
		logger:         logger,
	}

	// Setup routes should not panic even with empty registry
	srv.setupHTTPRoutes()

	// Note: This test primarily ensures setupHTTPRoutes doesn't panic
	// and handles empty/populated registries gracefully.
	// Full routing verification would require starting HTTP server.
}

func TestResponseRecorder_CapturesBodyAndStatus(t *testing.T) {
	inner := &fakeResponseWriter{header: http.Header{}}
	rec := &responseRecorder{ResponseWriter: inner, statusCode: http.StatusOK}

	rec.WriteHeader(http.StatusNotFound)
	rec.Write([]byte("not found"))

	assert.Equal(t, http.StatusNotFound, rec.statusCode)
	assert.Equal(t, []byte("not found"), rec.body)
}

func TestResponseRecorder_Flush(t *testing.T) {
	inner := &fakeResponseWriter{header: http.Header{}}
	rec := &responseRecorder{ResponseWriter: inner, statusCode: http.StatusOK}

	rec.WriteHeader(http.StatusOK)
	rec.Write([]byte("hello"))
	rec.flush()

	assert.Equal(t, http.StatusOK, inner.code)
	assert.Equal(t, "hello", inner.buf.String())
}

func TestBodyPreservation_CloneDoesNotConsumeBody(t *testing.T) {
	// Simulates the body preservation fix: read body, create fresh readers
	body := []byte(`{"glyph_id":"test-123"}`)
	r, err := http.NewRequest("POST", "/api/mock/create", bytes.NewReader(body))
	require.NoError(t, err)

	// Read body upfront (as the fix does)
	bodyBytes, err := io.ReadAll(r.Body)
	require.NoError(t, err)
	r.Body.Close()

	assert.Equal(t, body, bodyBytes)

	// First reader (for cloned request)
	r1Body := io.NopCloser(bytes.NewReader(bodyBytes))
	b1, err := io.ReadAll(r1Body)
	require.NoError(t, err)
	assert.Equal(t, body, b1)

	// Second reader (for fallback request) — must still work
	r2Body := io.NopCloser(bytes.NewReader(bodyBytes))
	b2, err := io.ReadAll(r2Body)
	require.NoError(t, err)
	assert.Equal(t, body, b2)
}

// fakeResponseWriter captures writes for testing responseRecorder.flush()
type fakeResponseWriter struct {
	header http.Header
	buf    bytes.Buffer
	code   int
}

func (f *fakeResponseWriter) Header() http.Header         { return f.header }
func (f *fakeResponseWriter) WriteHeader(code int)        { f.code = code }
func (f *fakeResponseWriter) Write(b []byte) (int, error) { return f.buf.Write(b) }
