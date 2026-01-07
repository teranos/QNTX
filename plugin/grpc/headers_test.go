package grpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap/zaptest"
)

func TestMultiValueHeaders_SetCookie(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()

	// Set up plugin to return multiple Set-Cookie headers
	plugin.httpHandlers["/cookies"] = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Set-Cookie", "session=abc123; Path=/; HttpOnly")
		w.Header().Add("Set-Cookie", "user=john; Path=/; Secure")
		w.Header().Add("Set-Cookie", "theme=dark; Path=/")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("cookies set"))
	}

	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)
	defer proxy.Close()

	services := &mockServiceRegistry{logger: logger}
	err = proxy.Initialize(context.Background(), services)
	require.NoError(t, err)

	// Create test HTTP server with proxy
	mux := http.NewServeMux()
	proxy.RegisterHTTP(mux)

	// Make request
	req := httptest.NewRequest("GET", "/api/mock/cookies", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Verify response
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify all Set-Cookie headers are preserved
	cookies := w.Header().Values("Set-Cookie")
	require.Len(t, cookies, 3, "All Set-Cookie headers should be preserved")
	assert.Contains(t, cookies, "session=abc123; Path=/; HttpOnly")
	assert.Contains(t, cookies, "user=john; Path=/; Secure")
	assert.Contains(t, cookies, "theme=dark; Path=/")
}

func TestMultiValueHeaders_Accept(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()

	// Capture request headers
	var receivedAccept []string
	plugin.httpHandlers["/accept"] = func(w http.ResponseWriter, r *http.Request) {
		receivedAccept = r.Header.Values("Accept")
		w.WriteHeader(http.StatusOK)
	}

	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)
	defer proxy.Close()

	services := &mockServiceRegistry{logger: logger}
	err = proxy.Initialize(context.Background(), services)
	require.NoError(t, err)

	// Create test HTTP server with proxy
	mux := http.NewServeMux()
	proxy.RegisterHTTP(mux)

	// Make request with multiple Accept headers
	req := httptest.NewRequest("GET", "/api/mock/accept", nil)
	req.Header.Add("Accept", "text/html")
	req.Header.Add("Accept", "application/json")
	req.Header.Add("Accept", "application/xml")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Verify all Accept headers were received
	require.Len(t, receivedAccept, 3, "All Accept headers should be preserved")
	assert.Contains(t, receivedAccept, "text/html")
	assert.Contains(t, receivedAccept, "application/json")
	assert.Contains(t, receivedAccept, "application/xml")
}

func TestSingleValueHeaders_NoRegression(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()

	var receivedContentType string
	var receivedAuth string
	plugin.httpHandlers["/single"] = func(w http.ResponseWriter, r *http.Request) {
		receivedContentType = r.Header.Get("Content-Type")
		receivedAuth = r.Header.Get("Authorization")

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Custom-Header", "test-value")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}

	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)
	defer proxy.Close()

	services := &mockServiceRegistry{logger: logger}
	err = proxy.Initialize(context.Background(), services)
	require.NoError(t, err)

	// Create test HTTP server with proxy
	mux := http.NewServeMux()
	proxy.RegisterHTTP(mux)

	// Make request with single-value headers
	req := httptest.NewRequest("POST", "/api/mock/single", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer token123")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Verify request headers were received
	assert.Equal(t, "application/json", receivedContentType)
	assert.Equal(t, "Bearer token123", receivedAuth)

	// Verify response headers
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Equal(t, "test-value", w.Header().Get("X-Custom-Header"))
}

func TestEmptyHeaderValues(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()
	server := NewPluginServer(plugin, logger)

	// Initialize server
	_, err := server.Initialize(context.Background(), &protocol.InitializeRequest{
		Config: map[string]string{},
	})
	require.NoError(t, err)

	// Test with empty header values
	req := &protocol.HTTPRequest{
		Method: "GET",
		Path:   "/test",
		Headers: []*protocol.HTTPHeader{
			{Name: "X-Empty", Values: []string{}},
			{Name: "X-Valid", Values: []string{"value"}},
		},
	}

	resp, err := server.HandleHTTP(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, int32(200), resp.StatusCode)
}

func TestHeaderCaseSensitivity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()

	var receivedHeaders http.Header
	plugin.httpHandlers["/case"] = func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}

	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)
	defer proxy.Close()

	services := &mockServiceRegistry{logger: logger}
	err = proxy.Initialize(context.Background(), services)
	require.NoError(t, err)

	// Create test HTTP server with proxy
	mux := http.NewServeMux()
	proxy.RegisterHTTP(mux)

	// Make request with various header cases
	req := httptest.NewRequest("GET", "/api/mock/case", nil)
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("X-Custom-Header", "value1")
	req.Header.Set("x-another-header", "value2")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// HTTP headers are case-insensitive, verify they're preserved
	assert.NotNil(t, receivedHeaders.Get("Content-Type"))
	assert.NotNil(t, receivedHeaders.Get("X-Custom-Header"))
	assert.NotNil(t, receivedHeaders.Get("x-another-header"))
}

func TestProtocolHeaderConversion(t *testing.T) {
	// Test conversion from http.Header to protocol.HTTPHeader
	httpHeaders := http.Header{
		"Content-Type": []string{"application/json"},
		"Set-Cookie":   []string{"a=1", "b=2", "c=3"},
		"Accept":       []string{"text/html", "application/json"},
	}

	protoHeaders := make([]*protocol.HTTPHeader, 0, len(httpHeaders))
	for name, values := range httpHeaders {
		protoHeaders = append(protoHeaders, &protocol.HTTPHeader{
			Name:   name,
			Values: values,
		})
	}

	// Verify conversion
	assert.Len(t, protoHeaders, 3)

	// Find and verify each header
	for _, header := range protoHeaders {
		switch header.Name {
		case "Content-Type":
			assert.Equal(t, []string{"application/json"}, header.Values)
		case "Set-Cookie":
			assert.Equal(t, []string{"a=1", "b=2", "c=3"}, header.Values)
		case "Accept":
			assert.Equal(t, []string{"text/html", "application/json"}, header.Values)
		}
	}

	// Test conversion back to http.Header
	resultHeaders := http.Header{}
	for _, header := range protoHeaders {
		for _, value := range header.Values {
			resultHeaders.Add(header.Name, value)
		}
	}

	// Verify round-trip conversion
	assert.Equal(t, httpHeaders.Values("Content-Type"), resultHeaders.Values("Content-Type"))
	assert.ElementsMatch(t, httpHeaders.Values("Set-Cookie"), resultHeaders.Values("Set-Cookie"))
	assert.ElementsMatch(t, httpHeaders.Values("Accept"), resultHeaders.Values("Accept"))
}
