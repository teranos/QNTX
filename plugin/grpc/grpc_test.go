package grpc

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	pluginpkg "github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// =============================================================================
// Test Fixtures
// =============================================================================

// mockPlugin is a configurable mock for testing
type mockPlugin struct {
	name           string
	initCalled     bool
	shutdownCalled bool
	initError      error
	shutdownError  error
	healthStatus   pluginpkg.HealthStatus
	httpHandlers   map[string]http.HandlerFunc
}

func newMockPlugin() *mockPlugin {
	return newMockPluginWithName("mock")
}

func newMockPluginWithName(name string) *mockPlugin {
	return &mockPlugin{
		name: name,
		healthStatus: pluginpkg.HealthStatus{
			Healthy: true,
			Message: "Mock plugin healthy",
			Details: map[string]interface{}{
				"test": "value",
			},
		},
		httpHandlers: make(map[string]http.HandlerFunc),
	}
}

func (p *mockPlugin) Metadata() pluginpkg.Metadata {
	return pluginpkg.Metadata{
		Name:        p.name,
		Version:     "1.0.0",
		QNTXVersion: ">= 0.1.0",
		Description: "Mock plugin for testing",
		Author:      "Test",
		License:     "MIT",
	}
}

func (p *mockPlugin) Initialize(ctx context.Context, services pluginpkg.ServiceRegistry) error {
	p.initCalled = true
	return p.initError
}

func (p *mockPlugin) Shutdown(ctx context.Context) error {
	p.shutdownCalled = true
	return p.shutdownError
}

func (p *mockPlugin) RegisterHTTP(mux *http.ServeMux) error {
	for path, handler := range p.httpHandlers {
		mux.HandleFunc(path, handler)
	}
	// Default handler - paths are stripped by proxy (e.g., /test not /api/mock/test)
	mux.HandleFunc("/test", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello from mock"))
	})
	return nil
}

func (p *mockPlugin) RegisterWebSocket() (map[string]pluginpkg.WebSocketHandler, error) {
	return nil, nil
}

func (p *mockPlugin) Health(ctx context.Context) pluginpkg.HealthStatus {
	return p.healthStatus
}

// mockServiceRegistry for testing
type mockServiceRegistry struct {
	logger *zap.SugaredLogger
}

func (m *mockServiceRegistry) Database() *sql.DB                       { return nil }
func (m *mockServiceRegistry) Logger(domain string) *zap.SugaredLogger { return m.logger }
func (m *mockServiceRegistry) Config(domain string) pluginpkg.Config   { return &mockConfig{} }
func (m *mockServiceRegistry) ATSStore() ats.AttestationStore          { return nil }
func (m *mockServiceRegistry) Queue() pluginpkg.QueueService           { return nil }

type mockConfig struct{}

func (c *mockConfig) GetString(key string) string        { return "" }
func (c *mockConfig) GetInt(key string) int              { return 0 }
func (c *mockConfig) GetBool(key string) bool            { return false }
func (c *mockConfig) GetStringSlice(key string) []string { return nil }
func (c *mockConfig) Get(key string) interface{}         { return nil }
func (c *mockConfig) Set(key string, value interface{})  {}
func (c *mockConfig) GetKeys() []string                  { return []string{} }

// startTestServer starts a gRPC server for testing and returns its address
func startTestServer(t *testing.T, plugin pluginpkg.DomainPlugin) (string, func()) {
	t.Helper()
	logger := zaptest.NewLogger(t).Sugar()
	server := NewPluginServer(plugin, logger)

	listener, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	grpcServer := grpc.NewServer()
	protocol.RegisterDomainPluginServiceServer(grpcServer, server)

	go func() {
		grpcServer.Serve(listener)
	}()

	// Wait for server to be ready
	time.Sleep(50 * time.Millisecond)

	cleanup := func() {
		grpcServer.Stop()
		listener.Close()
	}

	return listener.Addr().String(), cleanup
}

// =============================================================================
// PluginServer Unit Tests
// =============================================================================

func TestPluginServer_Metadata(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()
	server := NewPluginServer(plugin, logger)

	resp, err := server.Metadata(context.Background(), &protocol.Empty{})
	require.NoError(t, err)

	assert.Equal(t, "mock", resp.Name)
	assert.Equal(t, "1.0.0", resp.Version)
	assert.Equal(t, ">= 0.1.0", resp.QntxVersion)
	assert.Equal(t, "Mock plugin for testing", resp.Description)
	assert.Equal(t, "Test", resp.Author)
	assert.Equal(t, "MIT", resp.License)
}

func TestPluginServer_Health(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()
	server := NewPluginServer(plugin, logger)

	resp, err := server.Health(context.Background(), &protocol.Empty{})
	require.NoError(t, err)

	assert.True(t, resp.Healthy)
	assert.Equal(t, "Mock plugin healthy", resp.Message)
	assert.Equal(t, "value", resp.Details["test"])
}

func TestPluginServer_Health_Unhealthy(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()
	plugin.healthStatus = pluginpkg.HealthStatus{
		Healthy: false,
		Message: "Plugin is unhealthy",
		Details: map[string]interface{}{
			"error": "connection failed",
		},
	}
	server := NewPluginServer(plugin, logger)

	resp, err := server.Health(context.Background(), &protocol.Empty{})
	require.NoError(t, err)

	assert.False(t, resp.Healthy)
	assert.Equal(t, "Plugin is unhealthy", resp.Message)
	assert.Equal(t, "connection failed", resp.Details["error"])
}

func TestPluginServer_Initialize(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()
	server := NewPluginServer(plugin, logger)

	req := &protocol.InitializeRequest{
		Config: map[string]string{
			"key": "value",
		},
	}

	_, err := server.Initialize(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, plugin.initCalled)
}

func TestPluginServer_Initialize_Error(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()
	plugin.initError = errors.New("initialization failed")
	server := NewPluginServer(plugin, logger)

	req := &protocol.InitializeRequest{}
	_, err := server.Initialize(context.Background(), req)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "initialization failed")
}

func TestPluginServer_Shutdown(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()
	server := NewPluginServer(plugin, logger)

	_, err := server.Shutdown(context.Background(), &protocol.Empty{})
	require.NoError(t, err)
	assert.True(t, plugin.shutdownCalled)
}

func TestPluginServer_Shutdown_Error(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()
	plugin.shutdownError = errors.New("shutdown failed")
	server := NewPluginServer(plugin, logger)

	_, err := server.Shutdown(context.Background(), &protocol.Empty{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shutdown failed")
}

func TestPluginServer_HandleHTTP(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()
	server := NewPluginServer(plugin, logger)

	// Initialize to register HTTP handlers
	server.Initialize(context.Background(), &protocol.InitializeRequest{})

	// Test HTTP request - path is already stripped by proxy
	req := &protocol.HTTPRequest{
		Method: "GET",
		Path:   "/test",
		Headers: []*protocol.HTTPHeader{
			{Name: "Content-Type", Values: []string{"application/json"}},
		},
	}

	resp, err := server.HandleHTTP(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, int32(200), resp.StatusCode)
	assert.Equal(t, "hello from mock", string(resp.Body))
}

func TestPluginServer_HandleHTTP_POST(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()
	plugin.httpHandlers["/echo"] = func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		w.Header().Set("Content-Type", "application/json")
		w.Write(body)
	}
	server := NewPluginServer(plugin, logger)

	server.Initialize(context.Background(), &protocol.InitializeRequest{})

	req := &protocol.HTTPRequest{
		Method: "POST",
		Path:   "/echo",
		Headers: []*protocol.HTTPHeader{
			{Name: "Content-Type", Values: []string{"application/json"}},
		},
		Body: []byte(`{"message":"hello"}`),
	}

	resp, err := server.HandleHTTP(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, int32(200), resp.StatusCode)
	assert.Equal(t, `{"message":"hello"}`, string(resp.Body))
}

func TestPluginServer_HandleHTTP_NotFound(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()
	server := NewPluginServer(plugin, logger)

	server.Initialize(context.Background(), &protocol.InitializeRequest{})

	req := &protocol.HTTPRequest{
		Method: "GET",
		Path:   "/nonexistent",
	}

	resp, err := server.HandleHTTP(context.Background(), req)
	require.NoError(t, err)

	assert.Equal(t, int32(404), resp.StatusCode)
}

// =============================================================================
// ExternalDomainProxy Unit Tests
// =============================================================================

func TestExternalDomainProxy_Metadata(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	plugin := newMockPlugin()
	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	logger := zaptest.NewLogger(t).Sugar()
	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)
	defer proxy.Close()

	meta := proxy.Metadata()
	assert.Equal(t, "mock", meta.Name)
	assert.Equal(t, "1.0.0", meta.Version)
	assert.Equal(t, ">= 0.1.0", meta.QNTXVersion)
}

func TestExternalDomainProxy_Health(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	plugin := newMockPlugin()
	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	logger := zaptest.NewLogger(t).Sugar()
	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)
	defer proxy.Close()

	health := proxy.Health(context.Background())
	assert.True(t, health.Healthy)
	assert.Equal(t, "Mock plugin healthy", health.Message)
}

func TestExternalDomainProxy_Health_Unhealthy(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	plugin := newMockPlugin()
	plugin.healthStatus = pluginpkg.HealthStatus{
		Healthy: false,
		Message: "Database connection failed",
	}
	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	logger := zaptest.NewLogger(t).Sugar()
	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)
	defer proxy.Close()

	health := proxy.Health(context.Background())
	assert.False(t, health.Healthy)
	assert.Equal(t, "Database connection failed", health.Message)
}

func TestExternalDomainProxy_Initialize(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	plugin := newMockPlugin()
	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	logger := zaptest.NewLogger(t).Sugar()
	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)
	defer proxy.Close()

	services := &mockServiceRegistry{logger: logger}
	err = proxy.Initialize(context.Background(), services)
	require.NoError(t, err)
}

func TestExternalDomainProxy_Shutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	plugin := newMockPlugin()
	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	logger := zaptest.NewLogger(t).Sugar()
	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)

	err = proxy.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestExternalDomainProxy_RegisterHTTP(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	plugin := newMockPlugin()
	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	logger := zaptest.NewLogger(t).Sugar()
	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)
	defer proxy.Close()

	// Initialize first to set up HTTP handlers on the server side
	services := &mockServiceRegistry{logger: logger}
	err = proxy.Initialize(context.Background(), services)
	require.NoError(t, err)

	mux := http.NewServeMux()
	err = proxy.RegisterHTTP(mux)
	require.NoError(t, err)

	// Test that the handler is registered - use a path the mock plugin handles
	req := httptest.NewRequest("GET", "/api/mock/test", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// Handler should exist and return 200 with content from mock
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "hello from mock", w.Body.String())
}

func TestExternalDomainProxy_ImplementsDomainPlugin(t *testing.T) {
	// Compile-time check that ExternalDomainProxy implements DomainPlugin
	var _ pluginpkg.DomainPlugin = (*ExternalDomainProxy)(nil)
}

// =============================================================================
// RemoteServiceRegistry Unit Tests
// =============================================================================

func TestRemoteServiceRegistry_Database(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	registry := NewRemoteServiceRegistry("", "", "", nil, logger, nil)

	// Should return nil and log warning
	db := registry.Database()
	assert.Nil(t, db)
}

func TestRemoteServiceRegistry_ATSStore(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	registry := NewRemoteServiceRegistry("", "", "", nil, logger, nil)

	// Should return nil and log warning
	store := registry.ATSStore()
	assert.Nil(t, store)
}

func TestRemoteServiceRegistry_Queue(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	registry := NewRemoteServiceRegistry("", "", "", nil, logger, nil)

	// Should return nil and log warning
	queue := registry.Queue()
	assert.Nil(t, queue)
}

func TestRemoteServiceRegistry_Logger(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	registry := NewRemoteServiceRegistry("", "", "", nil, logger, nil)

	pluginLogger := registry.Logger("test")
	assert.NotNil(t, pluginLogger)
}

func TestRemoteServiceRegistry_Config(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	config := map[string]string{
		"key1":    "value1",
		"enabled": "true",
		"count":   "42",
	}
	registry := NewRemoteServiceRegistry("", "", "", config, logger, nil)

	cfg := registry.Config("test")
	assert.Equal(t, "value1", cfg.GetString("key1"))
	assert.True(t, cfg.GetBool("enabled"))
	assert.Empty(t, cfg.GetString("nonexistent"))
}

func TestRemoteConfig_GetBool(t *testing.T) {
	tests := []struct {
		value    string
		expected bool
	}{
		{"true", true},
		{"1", true},
		{"yes", true}, // Now permissive - accepts yes as true
		{"false", false},
		{"0", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			cfg := newRemoteConfig("test", map[string]string{"key": tt.value})
			assert.Equal(t, tt.expected, cfg.GetBool("key"))
		})
	}
}

func TestRemoteConfig_Set(t *testing.T) {
	cfg := newRemoteConfig("test", make(map[string]string))
	cfg.Set("key", "value")
	assert.Equal(t, "value", cfg.GetString("key"))
}

// =============================================================================
// Integration Tests
// =============================================================================

func TestPluginClientServer_FullIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()
	plugin.httpHandlers["/data"] = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	}

	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	// Connect proxy
	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)
	defer proxy.Close()

	// Test 1: Metadata
	meta := proxy.Metadata()
	assert.Equal(t, "mock", meta.Name)

	// Test 2: Initialize
	services := &mockServiceRegistry{logger: logger}
	err = proxy.Initialize(context.Background(), services)
	require.NoError(t, err)

	// Test 3: Health
	health := proxy.Health(context.Background())
	assert.True(t, health.Healthy)

	// Test 4: Shutdown
	err = proxy.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestPluginClientServer_HTTPProxying(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()
	plugin.httpHandlers["/json"] = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"key":"value"}`))
	}

	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)
	defer proxy.Close()

	// Initialize to set up HTTP handlers
	services := &mockServiceRegistry{logger: logger}
	proxy.Initialize(context.Background(), services)

	// Create test server with proxy handlers
	mux := http.NewServeMux()
	proxy.RegisterHTTP(mux)

	// Make request through proxy
	req := httptest.NewRequest("GET", "/api/mock/json", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, `{"key":"value"}`, w.Body.String())
}

func TestPluginClientServer_MultiplePlugins(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()

	// Start multiple plugin servers
	plugin1 := newMockPlugin()
	plugin2 := newMockPlugin()

	addr1, cleanup1 := startTestServer(t, plugin1)
	defer cleanup1()

	addr2, cleanup2 := startTestServer(t, plugin2)
	defer cleanup2()

	// Connect to both
	proxy1, err := NewExternalDomainProxy(addr1, logger)
	require.NoError(t, err)
	defer proxy1.Close()

	proxy2, err := NewExternalDomainProxy(addr2, logger)
	require.NoError(t, err)
	defer proxy2.Close()

	// Both should work independently
	health1 := proxy1.Health(context.Background())
	health2 := proxy2.Health(context.Background())

	assert.True(t, health1.Healthy)
	assert.True(t, health2.Healthy)
}

// =============================================================================
// PluginManager Tests
// =============================================================================

func TestPluginManager_NewPluginManager(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	manager := NewPluginManager(logger)

	assert.NotNil(t, manager)
	assert.Empty(t, manager.GetAllPlugins())
}

func TestPluginManager_LoadPlugins_Disabled(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	manager := NewPluginManager(logger)

	configs := []PluginConfig{
		{Name: "disabled", Enabled: false},
	}

	err := manager.LoadPlugins(context.Background(), configs)
	require.NoError(t, err)
	assert.Empty(t, manager.GetAllPlugins())
}

func TestPluginManager_LoadPlugins_InvalidConfig(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	manager := NewPluginManager(logger)

	configs := []PluginConfig{
		{Name: "invalid", Enabled: true}, // Neither address nor binary
	}

	// Should not return error - logs warning and continues
	err := manager.LoadPlugins(context.Background(), configs)
	require.NoError(t, err)

	// Plugin should not be loaded
	plugins := manager.GetAllPlugins()
	assert.Len(t, plugins, 0)
}

func TestPluginManager_LoadPlugins_WithAddress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()

	// Start a plugin server
	plugin := newMockPlugin()
	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	manager := NewPluginManager(logger)
	configs := []PluginConfig{
		{Name: "mock", Enabled: true, Address: addr}, // Use "mock" to match the plugin metadata
	}

	err := manager.LoadPlugins(context.Background(), configs)
	require.NoError(t, err)

	plugins := manager.GetAllPlugins()
	assert.Len(t, plugins, 1)

	// Verify the plugin works
	meta := plugins[0].Metadata()
	assert.Equal(t, "mock", meta.Name)
}

func TestPluginManager_GetPlugin(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()

	plugin := newMockPluginWithName("test")
	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	manager := NewPluginManager(logger)
	configs := []PluginConfig{
		{Name: "test", Enabled: true, Address: addr},
	}
	manager.LoadPlugins(context.Background(), configs)

	// Get existing plugin
	p, ok := manager.GetPlugin("test")
	assert.True(t, ok)
	assert.NotNil(t, p)

	// Get non-existing plugin
	_, ok = manager.GetPlugin("nonexistent")
	assert.False(t, ok)
}

func TestPluginManager_Shutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()

	plugin := newMockPlugin()
	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	manager := NewPluginManager(logger)
	configs := []PluginConfig{
		{Name: "test", Enabled: true, Address: addr},
	}
	manager.LoadPlugins(context.Background(), configs)

	err := manager.Shutdown(context.Background())
	require.NoError(t, err)

	// Plugins should be cleared
	assert.Empty(t, manager.GetAllPlugins())
}

// =============================================================================
// Edge Cases and Error Handling
// =============================================================================

func TestExternalDomainProxy_ContextCancellation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	plugin := newMockPlugin()
	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	logger := zaptest.NewLogger(t).Sugar()
	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)
	defer proxy.Close()

	// Test with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	health := proxy.Health(ctx)
	// Should return unhealthy due to context cancellation
	assert.False(t, health.Healthy)
}

func TestPluginServer_ConcurrentRequests(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	plugin := newMockPlugin()
	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	logger := zaptest.NewLogger(t).Sugar()
	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)
	defer proxy.Close()

	// Make concurrent requests
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			health := proxy.Health(context.Background())
			assert.True(t, health.Healthy)
			done <- true
		}()
	}

	// Wait for all to complete
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestPluginConfig_Defaults(t *testing.T) {
	cfg := PluginConfig{
		Name:    "test",
		Enabled: true,
	}

	assert.Equal(t, "test", cfg.Name)
	assert.True(t, cfg.Enabled)
	assert.Empty(t, cfg.Address)
	assert.Empty(t, cfg.Binary)
	assert.False(t, cfg.AutoStart)
}

func TestDiscoverPlugins_EmptyDir(t *testing.T) {
	// Test with non-existent directory
	configs, err := DiscoverPlugins("/nonexistent/path")
	require.NoError(t, err)
	assert.Empty(t, configs)
}

// TestPluginServer_WebSocketStreaming tests WebSocket bidirectional streaming
func TestPluginServer_WebSocketStreaming(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()

	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	// Create external domain proxy
	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)
	defer proxy.Close()

	// Initialize plugin
	services := &mockServiceRegistry{logger: logger}
	err = proxy.Initialize(context.Background(), services)
	require.NoError(t, err)

	// Establish gRPC stream directly to test the HandleWebSocket implementation
	stream, err := proxy.client.HandleWebSocket(context.Background())
	require.NoError(t, err)

	// Send CONNECT message
	err = stream.Send(&protocol.WebSocketMessage{
		Type: protocol.WebSocketMessage_CONNECT,
		Data: []byte{},
	})
	require.NoError(t, err)

	// Send test data
	testMessage := []byte("Hello from WebSocket client")
	err = stream.Send(&protocol.WebSocketMessage{
		Type: protocol.WebSocketMessage_DATA,
		Data: testMessage,
	})
	require.NoError(t, err)

	// Receive echoed data (server echoes back)
	msg, err := stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, protocol.WebSocketMessage_DATA, msg.Type)
	assert.Equal(t, testMessage, msg.Data)

	// Send multiple messages to test bidirectional streaming
	for i := 0; i < 5; i++ {
		testMsg := []byte(fmt.Sprintf("Message %d", i))
		err = stream.Send(&protocol.WebSocketMessage{
			Type: protocol.WebSocketMessage_DATA,
			Data: testMsg,
		})
		require.NoError(t, err)

		// Receive echo
		msg, err := stream.Recv()
		require.NoError(t, err)
		assert.Equal(t, protocol.WebSocketMessage_DATA, msg.Type)
		assert.Equal(t, testMsg, msg.Data)
	}

	// Send CLOSE message
	err = stream.Send(&protocol.WebSocketMessage{
		Type: protocol.WebSocketMessage_CLOSE,
		Data: []byte{},
	})
	require.NoError(t, err)

	// Receive CLOSE acknowledgment
	msg, err = stream.Recv()
	require.NoError(t, err)
	assert.Equal(t, protocol.WebSocketMessage_CLOSE, msg.Type)

	// Close the stream
	err = stream.CloseSend()
	require.NoError(t, err)
}

// TestPluginServer_PortAutoIncrement verifies that the server automatically
// increments to the next available port when the requested port is occupied.
func TestPluginServer_PortAutoIncrement(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()
	server := NewPluginServer(plugin, logger)

	// Find a free port to use as base
	baseListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	baseAddr := baseListener.Addr().String()
	_, basePortStr, err := net.SplitHostPort(baseAddr)
	require.NoError(t, err)
	baseListener.Close()

	// Occupy the base port
	occupiedListener, err := net.Listen("tcp", baseAddr)
	require.NoError(t, err)
	defer occupiedListener.Close()

	t.Logf("Base port %s is occupied, server should auto-increment", basePortStr)

	// Try to start server on the occupied port
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Run server in goroutine
	serverErrChan := make(chan error, 1)
	serverStarted := make(chan string, 1)

	go func() {
		// The server should automatically increment to basePort+1
		err := server.Serve(ctx, baseAddr)
		serverErrChan <- err
	}()

	// Give server time to start and announce port
	time.Sleep(500 * time.Millisecond)

	// Try to connect to basePort+1 (where server should have bound)
	expectedPort := mustParsePort(t, basePortStr) + 1
	expectedAddr := fmt.Sprintf("127.0.0.1:%d", expectedPort)

	connCtx, connCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer connCancel()
	conn, err := grpc.DialContext(connCtx, expectedAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock())
	require.NoError(t, err, "Server should have bound to port %d", expectedPort)
	defer conn.Close()

	// Verify the server is responding
	client := protocol.NewDomainPluginServiceClient(conn)
	resp, err := client.Metadata(context.Background(), &protocol.Empty{})
	require.NoError(t, err)
	assert.Equal(t, "mock", resp.Name)

	t.Logf("Successfully connected to server on auto-incremented port %d", expectedPort)

	// Send serverStarted notification
	serverStarted <- expectedAddr

	// Cleanup
	cancel()
	select {
	case err := <-serverErrChan:
		// Server error is expected when context is cancelled
		t.Logf("Server stopped: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("Server did not stop after context cancellation")
	}
}

// mustParsePort is a test helper to parse port from string
func mustParsePort(t *testing.T, portStr string) int {
	var port int
	_, err := fmt.Sscanf(portStr, "%d", &port)
	require.NoError(t, err)
	return port
}
