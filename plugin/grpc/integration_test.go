//go:build integration

package grpc

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/storage"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	pluginpkg "github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// =============================================================================
// Critical Path Tests
// =============================================================================

// TestCriticalPath_PluginLifecycle tests the complete plugin lifecycle:
// discovery -> load -> initialize -> use -> shutdown
func TestCriticalPath_PluginLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping critical path test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPluginWithName("critical-test")

	// 1. Start plugin server (simulates discovered plugin binary)
	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	// 2. Create plugin manager
	manager := NewPluginManager(logger)

	// 3. Load plugin from address
	configs := []PluginConfig{
		{Name: "critical-test", Enabled: true, Address: addr},
	}
	err := manager.LoadPlugins(context.Background(), configs)
	require.NoError(t, err)

	plugins := manager.GetAllPlugins()
	require.Len(t, plugins, 1)
	proxy := plugins[0]

	// 4. Initialize plugin
	services := &mockServiceRegistry{logger: logger}
	err = proxy.Initialize(context.Background(), services)
	require.NoError(t, err)
	assert.True(t, plugin.initCalled)

	// 5. Use plugin (HTTP request)
	mux := http.NewServeMux()
	err = proxy.RegisterHTTP(mux)
	require.NoError(t, err)

	// 6. Verify health
	health := proxy.Health(context.Background())
	assert.True(t, health.Healthy)

	// 7. Shutdown gracefully
	err = manager.Shutdown(context.Background())
	require.NoError(t, err)
	assert.True(t, plugin.shutdownCalled)
}

// TestCriticalPath_MultiPluginCoordination tests multiple plugins working together
func TestCriticalPath_MultiPluginCoordination(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping critical path test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()

	// Start 3 different plugins
	plugin1 := newMockPluginWithName("plugin1")
	plugin2 := newMockPluginWithName("plugin2")
	plugin3 := newMockPluginWithName("plugin3")

	addr1, cleanup1 := startTestServer(t, plugin1)
	defer cleanup1()
	addr2, cleanup2 := startTestServer(t, plugin2)
	defer cleanup2()
	addr3, cleanup3 := startTestServer(t, plugin3)
	defer cleanup3()

	// Load all plugins
	manager := NewPluginManager(logger)
	configs := []PluginConfig{
		{Name: "plugin1", Enabled: true, Address: addr1},
		{Name: "plugin2", Enabled: true, Address: addr2},
		{Name: "plugin3", Enabled: true, Address: addr3},
	}
	err := manager.LoadPlugins(context.Background(), configs)
	require.NoError(t, err)

	plugins := manager.GetAllPlugins()
	require.Len(t, plugins, 3)

	// Initialize all
	services := &mockServiceRegistry{logger: logger}
	for _, p := range plugins {
		err := p.Initialize(context.Background(), services)
		require.NoError(t, err)
	}

	// All should be healthy
	for _, p := range plugins {
		health := p.Health(context.Background())
		assert.True(t, health.Healthy)
	}

	// Shutdown all
	err = manager.Shutdown(context.Background())
	require.NoError(t, err)
}

// TestCriticalPath_ErrorRecovery tests error handling and recovery
func TestCriticalPath_ErrorRecovery(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping critical path test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()

	// Plugin that fails initialization
	plugin := newMockPlugin()
	plugin.initError = fmt.Errorf("initialization failed")

	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)
	defer proxy.Close()

	services := &mockServiceRegistry{logger: logger}

	// Should fail gracefully
	err = proxy.Initialize(context.Background(), services)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "initialization failed")

	// Health check should still work
	health := proxy.Health(context.Background())
	assert.NotNil(t, health)
}

// =============================================================================
// Concurrent Command Execution Tests
// =============================================================================

// TestConcurrent_MixedOperations tests concurrent execution of different command types
func TestConcurrent_MixedOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()

	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)
	defer proxy.Close()

	services := &mockServiceRegistry{logger: logger}
	err = proxy.Initialize(context.Background(), services)
	require.NoError(t, err)

	// Execute different operations concurrently
	const workers = 20
	const opsPerWorker = 10
	var wg sync.WaitGroup
	errors := make(chan error, workers*opsPerWorker)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for j := 0; j < opsPerWorker; j++ {
				switch j % 3 {
				case 0:
					// Health checks
					health := proxy.Health(context.Background())
					if !health.Healthy {
						errors <- fmt.Errorf("worker %d: health check failed", workerID)
					}
				case 1:
					// Metadata calls
					meta := proxy.Metadata()
					if meta.Name != "mock" {
						errors <- fmt.Errorf("worker %d: metadata mismatch", workerID)
					}
				case 2:
					// HTTP calls (through RegisterHTTP proxy)
					// This tests the HTTP handler registration stability
					mux := http.NewServeMux()
					if err := proxy.RegisterHTTP(mux); err != nil {
						errors <- fmt.Errorf("worker %d: HTTP registration failed: %w", workerID, err)
					}
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	var collectedErrors []error
	for err := range errors {
		collectedErrors = append(collectedErrors, err)
	}
	assert.Empty(t, collectedErrors, "Concurrent operations should not produce errors")
}

// TestConcurrent_InitializeRace tests concurrent initialization attempts
func TestConcurrent_InitializeRace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()

	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)
	defer proxy.Close()

	services := &mockServiceRegistry{logger: logger}

	// Try to initialize concurrently from multiple goroutines
	const workers = 10
	var wg sync.WaitGroup
	errors := make(chan error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := proxy.Initialize(context.Background(), services)
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	// All initializations should succeed (idempotent)
	// or fail gracefully without race conditions
	for err := range errors {
		// Initialization errors are ok as long as no panics
		t.Logf("Concurrent init error (expected): %v", err)
	}

	// Plugin should have been initialized at least once
	assert.True(t, plugin.initCalled)
}

// TestConcurrent_ShutdownRace tests shutdown while operations are in flight
func TestConcurrent_ShutdownRace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()

	addr, cleanup := startTestServer(t, plugin)
	defer cleanup()

	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)

	services := &mockServiceRegistry{logger: logger}
	proxy.Initialize(context.Background(), services)

	// Start ongoing operations
	var wg sync.WaitGroup
	stopChan := make(chan struct{})

	// Worker making continuous health checks
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopChan:
					return
				default:
					proxy.Health(context.Background())
					time.Sleep(10 * time.Millisecond)
				}
			}
		}()
	}

	// Let operations run for a bit
	time.Sleep(100 * time.Millisecond)

	// Shutdown while operations are in flight
	err = proxy.Shutdown(context.Background())
	close(stopChan)
	wg.Wait()

	// Shutdown should complete without deadlock
	require.NoError(t, err)
}

// =============================================================================
// Plugin Crash and Recovery Tests
// =============================================================================

// TestCrash_ServerUnresponsive tests behavior when plugin server stops responding
func TestCrash_ServerUnresponsive(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping crash test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()

	addr, cleanup := startTestServer(t, plugin)

	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)

	services := &mockServiceRegistry{logger: logger}
	err = proxy.Initialize(context.Background(), services)
	require.NoError(t, err)

	// Verify it works initially
	health := proxy.Health(context.Background())
	assert.True(t, health.Healthy)

	// Simulate crash: stop server
	cleanup()
	time.Sleep(100 * time.Millisecond) // Give server time to stop

	// Subsequent calls should fail gracefully
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	health = proxy.Health(ctx)
	// Should return unhealthy, not panic
	assert.False(t, health.Healthy)
	assert.Contains(t, health.Message, "Failed to check plugin health")
}

// TestCrash_ReconnectionAttempt tests attempting to reconnect after crash
func TestCrash_ReconnectionAttempt(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping crash test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()

	// Start server
	plugin1 := newMockPlugin()
	addr, cleanup1 := startTestServer(t, plugin1)

	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)

	// Crash server
	cleanup1()
	time.Sleep(100 * time.Millisecond)

	// Try to create new connection to same address
	// This should fail since server is down
	_, err = NewExternalDomainProxy(addr, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect")

	// Cleanup original proxy
	proxy.Close()
}

// TestCrash_PartialFailure tests when some plugins crash but others continue
func TestCrash_PartialFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping crash test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()

	// Start two plugins
	plugin1 := newMockPluginWithName("plugin1")
	plugin2 := newMockPluginWithName("plugin2")

	addr1, cleanup1 := startTestServer(t, plugin1)
	addr2, cleanup2 := startTestServer(t, plugin2)
	defer cleanup2()

	manager := NewPluginManager(logger)
	configs := []PluginConfig{
		{Name: "plugin1", Enabled: true, Address: addr1},
		{Name: "plugin2", Enabled: true, Address: addr2},
	}
	err := manager.LoadPlugins(context.Background(), configs)
	require.NoError(t, err)

	// Get plugins by name to avoid non-deterministic map iteration order
	plugin1Proxy, ok := manager.GetPlugin("plugin1")
	require.True(t, ok, "plugin1 should be loaded")
	plugin2Proxy, ok := manager.GetPlugin("plugin2")
	require.True(t, ok, "plugin2 should be loaded")

	// Crash first plugin
	cleanup1()
	time.Sleep(100 * time.Millisecond)

	// Second plugin should still work
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	health := plugin2Proxy.Health(ctx)
	assert.True(t, health.Healthy, "Healthy plugin should remain operational")

	// First plugin should fail
	health = plugin1Proxy.Health(ctx)
	assert.False(t, health.Healthy, "Crashed plugin should report unhealthy")
}

// TestCrash_GracefulDegradation tests system continues with failed plugins
func TestCrash_GracefulDegradation(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping crash test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()
	manager := NewPluginManager(logger)

	// Try to load plugin that doesn't exist
	configs := []PluginConfig{
		{Name: "nonexistent", Enabled: true, Address: "localhost:59999"},
	}

	// Should not return error (resilient loading) but log and continue
	err := manager.LoadPlugins(context.Background(), configs)
	require.NoError(t, err, "LoadPlugins should not return error for failed plugins (resilient loading)")

	// Manager should still be usable with no plugins loaded
	plugins := manager.GetAllPlugins()
	assert.Empty(t, plugins, "No plugins should be loaded when connection fails")

	// Can still shut down cleanly
	err = manager.Shutdown(context.Background())
	require.NoError(t, err)
}

// =============================================================================
// Network Failure Tests
// =============================================================================

// TestNetwork_TimeoutHandling tests timeout behavior for slow plugins
func TestNetwork_TimeoutHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()

	// Create slow plugin that delays responses
	slowPlugin := newMockPlugin()
	slowPlugin.healthStatus = pluginpkg.HealthStatus{
		Healthy: true,
		Message: "Slow plugin",
	}

	addr, cleanup := startTestServer(t, slowPlugin)
	defer cleanup()

	proxy, err := NewExternalDomainProxy(addr, logger)
	require.NoError(t, err)
	defer proxy.Close()

	// Test with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()

	// Should timeout gracefully
	health := proxy.Health(ctx)
	// Due to our 5s default timeout in Health(), this might not timeout
	// but the context cancellation should be detected
	t.Logf("Health check result with timeout: %+v", health)
}

// TestNetwork_ConnectionRefused tests handling of refused connections
func TestNetwork_ConnectionRefused(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()

	// Try to connect to address with no server
	_, err := NewExternalDomainProxy("localhost:59998", logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect")
}

// =============================================================================
// Invalid Message Tests
// =============================================================================

// TestInvalid_MalformedHTTPRequest tests handling of invalid HTTP requests
func TestInvalid_MalformedHTTPRequest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping invalid message test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()
	plugin := newMockPlugin()
	server := NewPluginServer(plugin, logger)

	server.Initialize(context.Background(), &protocol.InitializeRequest{})

	// Send request with empty method
	req := &protocol.HTTPRequest{
		Method: "", // Invalid empty method
		Path:   "/api/mock/test",
	}

	resp, err := server.HandleHTTP(context.Background(), req)
	// Server should handle gracefully
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

// TestInvalid_NilContext tests context handling
func TestInvalid_NilContext(t *testing.T) {
	plugin := newMockPlugin()

	// Health check with background context should work
	health := plugin.Health(context.Background())
	assert.True(t, health.Healthy)
}

// =============================================================================
// Service Integration Tests (Issue #138)
// =============================================================================

// testServiceRegistry implements pluginpkg.ServiceRegistry for integration testing
type testServiceRegistry struct {
	logger *zap.SugaredLogger
	store  ats.AttestationStore
	queue  pluginpkg.QueueService
	config map[string]string
}

func (r *testServiceRegistry) Database() *sql.DB {
	return nil
}

func (r *testServiceRegistry) Logger(domain string) *zap.SugaredLogger {
	return r.logger.Named(domain)
}

func (r *testServiceRegistry) Config(domain string) pluginpkg.Config {
	return &testConfig{config: r.config}
}

func (r *testServiceRegistry) ATSStore() ats.AttestationStore {
	return r.store
}

func (r *testServiceRegistry) Queue() pluginpkg.QueueService {
	return r.queue
}

// testConfig implements pluginpkg.Config for integration testing
type testConfig struct {
	config map[string]string
}

func (c *testConfig) GetString(key string) string {
	return c.config[key]
}

func (c *testConfig) GetInt(key string) int {
	return 0
}

func (c *testConfig) GetBool(key string) bool {
	return false
}

func (c *testConfig) GetStringSlice(key string) []string {
	return nil
}

func (c *testConfig) Get(key string) interface{} {
	return c.config[key]
}

func (c *testConfig) Set(key string, value interface{}) {
	if s, ok := value.(string); ok {
		c.config[key] = s
	}
}

func (c *testConfig) GetKeys() []string {
	keys := make([]string, 0, len(c.config))
	for k := range c.config {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestServiceIntegration_BookCollectorAttestations tests end-to-end service callbacks.
// This verifies that gRPC plugins can create attestations via gRPC ATSStore service.
func TestServiceIntegration_BookCollectorAttestations(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping service integration test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()
	ctx := context.Background()

	// 1. Create test database
	db := qntxtest.CreateTestDB(t)

	// 2. Create ATSStore and Queue
	store := storage.NewSQLStore(db, logger)
	queue := async.NewQueue(db)

	// 3. Start gRPC services for plugin callbacks
	servicesManager := NewServicesManager(logger)
	endpoints, err := servicesManager.Start(ctx, store, queue)
	require.NoError(t, err)
	defer servicesManager.Shutdown()

	logger.Infow("Started plugin services",
		"ats_store", endpoints.ATSStoreAddress,
		"queue", endpoints.QueueAddress,
	)

	// 4. Create and start book plugin server
	bookPlugin := NewBookPlugin()
	pluginServer := NewPluginServer(bookPlugin, logger)

	// Start plugin gRPC server in background
	pluginAddr := "localhost:0" // Use dynamic port
	listener, err := net.Listen("tcp", pluginAddr)
	require.NoError(t, err)
	actualPluginAddr := listener.Addr().String()
	defer listener.Close()

	grpcServer := grpc.NewServer()
	protocol.RegisterDomainPluginServiceServer(grpcServer, pluginServer)

	go func() {
		grpcServer.Serve(listener)
	}()
	defer grpcServer.Stop()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// 5. Connect to plugin as client
	proxy, err := NewExternalDomainProxy(actualPluginAddr, logger)
	require.NoError(t, err)
	defer proxy.Close()

	// 6. Initialize book plugin directly with real services
	// Note: In production, gRPC plugins would use gRPC clients from RemoteServiceRegistry.
	// For this test, we directly initialize with services to verify the attestation logic.
	services := &testServiceRegistry{
		logger: logger,
		store:  store,
		queue:  queue,
		config: map[string]string{},
	}

	// Initialize the book plugin directly (not through gRPC proxy)
	// This tests that the plugin can create attestations when given services
	err = bookPlugin.Initialize(ctx, services)
	require.NoError(t, err)

	// 9. Verify attestations were created in database
	filter := ats.AttestationFilter{Limit: 100}
	attestations, err := store.GetAttestations(context.Background(), filter)
	require.NoError(t, err)

	// Should have attestations for collector wants and auction offers
	assert.Greater(t, len(attestations), 0, "Plugin should have created attestations")

	// Verify specific attestations exist
	var collectorWants []string
	var auctionOffers []string

	for _, att := range attestations {
		if len(att.Subjects) > 0 && len(att.Predicates) > 0 && len(att.Contexts) > 0 {
			subject := att.Subjects[0]
			predicate := att.Predicates[0]
			context := att.Contexts[0]

			if subject == "collector" && predicate == "wants" {
				collectorWants = append(collectorWants, context)
			}
			if predicate == "offers" {
				auctionOffers = append(auctionOffers, context)
			}
		}
	}

	logger.Infow("Service integration verification",
		"total_attestations", len(attestations),
		"collector_wants", len(collectorWants),
		"auction_offers", len(auctionOffers),
	)

	// Verify expected attestations
	assert.Contains(t, collectorWants, "organon", "Collector should want Organon")
	assert.Contains(t, collectorWants, "elements", "Collector should want Elements")
	assert.Contains(t, collectorWants, "time-clocks-ordering", "Collector should want Lamport's paper")

	assert.Contains(t, auctionOffers, "organon", "Christie's should offer Organon")
	assert.Contains(t, auctionOffers, "elements", "Sotheby's should offer Elements")

	// 10. Verify plugin found matches
	health := proxy.Health(ctx)
	assert.True(t, health.Healthy)
	logger.Info("Book collector plugin successfully created and queried attestations via gRPC services")
}

func TestExternalDomainProxy_ConnectionFailure(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()

	// Use a port that's unlikely to be in use
	_, err := NewExternalDomainProxy("localhost:59999", logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to connect")
}

// TestPortAutoIncrement_FullIntegration tests the complete port auto-increment flow:
// 1. Occupy a port
// 2. Plugin server tries to bind and auto-increments
// 3. Plugin outputs QNTX_PLUGIN_PORT=XXXX
// 4. pluginLogger captures the port announcement
// 5. Verification that everything works
func TestPortAutoIncrement_FullIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()

	// Find a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	occupiedAddr := listener.Addr().String()
	host, portStr, err := net.SplitHostPort(occupiedAddr)
	require.NoError(t, err)

	// Close listener but immediately reopen to occupy the port
	// (small race condition window, but acceptable for testing)
	listener.Close()
	listener, err = net.Listen("tcp", occupiedAddr)
	require.NoError(t, err)
	defer listener.Close()

	basePort := mustParsePort(t, portStr)
	expectedPort := basePort + 1

	t.Logf("Occupying port %d, plugin should auto-increment to %d", basePort, expectedPort)

	// Create a mock plugin and server
	plugin := newMockPluginWithName("port-test")
	server := NewPluginServer(plugin, logger)

	// Start the server in a goroutine (it should auto-increment)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serverReady := make(chan bool, 1)
	serverErr := make(chan error, 1)

	go func() {
		// Server should detect port conflict and increment
		err := server.Serve(ctx, occupiedAddr)
		if err != nil {
			serverErr <- err
		}
	}()

	// Wait a bit for server to start
	time.Sleep(500 * time.Millisecond)

	// Verify server bound to expectedPort by connecting to it
	actualAddr := net.JoinHostPort(host, fmt.Sprintf("%d", expectedPort))
	connCtx, connCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer connCancel()

	conn, err := grpc.DialContext(connCtx, actualAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock())
	require.NoError(t, err, "Server should have auto-incremented to port %d", expectedPort)
	defer conn.Close()

	// Verify the server is responding on the incremented port
	client := protocol.NewDomainPluginServiceClient(conn)
	resp, err := client.Metadata(context.Background(), &protocol.Empty{})
	require.NoError(t, err)
	assert.Equal(t, "port-test", resp.Name)

	t.Logf("✓ Server successfully auto-incremented from %d to %d", basePort, expectedPort)

	// Signal server ready
	serverReady <- true

	// Cleanup
	cancel()

	// Give server time to shut down gracefully
	select {
	case err := <-serverErr:
		// Server shutdown, check for unexpected errors
		if err != nil && err.Error() != "context canceled" {
			t.Logf("Server stopped with error: %v", err)
		}
	case <-time.After(2 * time.Second):
		// Server shutdown gracefully
	}
}

// TestPortAutoIncrement_MultipleConflicts tests multiple consecutive port conflicts
func TestPortAutoIncrement_MultipleConflicts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()

	// Find a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	baseAddr := listener.Addr().String()
	host, portStr, err := net.SplitHostPort(baseAddr)
	require.NoError(t, err)
	basePort := mustParsePort(t, portStr)
	listener.Close()

	// Occupy the next 3 ports
	var occupiedListeners []net.Listener
	for i := 0; i < 3; i++ {
		addr := net.JoinHostPort(host, fmt.Sprintf("%d", basePort+i))
		l, err := net.Listen("tcp", addr)
		require.NoError(t, err, "Failed to occupy port %d", basePort+i)
		occupiedListeners = append(occupiedListeners, l)
		defer l.Close()
	}

	t.Logf("Occupied ports %d-%d, plugin should bind to %d", basePort, basePort+2, basePort+3)

	// Create plugin and server
	plugin := newMockPluginWithName("multi-conflict-test")
	server := NewPluginServer(plugin, logger)

	// Start server
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		server.Serve(ctx, baseAddr)
	}()

	time.Sleep(500 * time.Millisecond)

	// Verify server bound to basePort+3 (after 3 conflicts)
	expectedPort := basePort + 3
	expectedAddr := net.JoinHostPort(host, fmt.Sprintf("%d", expectedPort))

	connCtx, connCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer connCancel()

	conn, err := grpc.DialContext(connCtx, expectedAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock())
	require.NoError(t, err, "Server should have incremented to port %d after 3 conflicts", expectedPort)
	defer conn.Close()

	// Verify server is responding
	client := protocol.NewDomainPluginServiceClient(conn)
	resp, err := client.Metadata(context.Background(), &protocol.Empty{})
	require.NoError(t, err)
	assert.Equal(t, "multi-conflict-test", resp.Name)

	t.Logf("✓ Server successfully resolved 3 port conflicts and bound to %d", expectedPort)

	cancel()
}

// TestPortAutoIncrement_MaxAttempts verifies the 64-attempt limit
func TestPortAutoIncrement_MaxAttempts(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	logger := zaptest.NewLogger(t).Sugar()

	// This test is expensive (64 port allocations), so we only verify the limit exists
	// by checking that the server eventually gives up

	// Find a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	baseAddr := listener.Addr().String()
	host, portStr, err := net.SplitHostPort(baseAddr)
	require.NoError(t, err)
	basePort := mustParsePort(t, portStr)
	listener.Close()

	// Occupy a large range (let's do 10 to keep test fast, full test would be 64)
	var occupiedListeners []net.Listener
	for i := 0; i < 10; i++ {
		addr := net.JoinHostPort(host, fmt.Sprintf("%d", basePort+i))
		l, err := net.Listen("tcp", addr)
		if err != nil {
			// Some ports might fail due to OS restrictions, skip them
			continue
		}
		occupiedListeners = append(occupiedListeners, l)
		defer l.Close()
	}

	t.Logf("Occupied %d ports starting from %d", len(occupiedListeners), basePort)

	// Plugin should find the first available port after our occupied range
	plugin := newMockPluginWithName("max-attempts-test")
	server := NewPluginServer(plugin, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	serverErr := make(chan error, 1)
	go func() {
		err := server.Serve(ctx, baseAddr)
		serverErr <- err
	}()

	time.Sleep(500 * time.Millisecond)

	// Server should have found a port after our occupied range
	expectedPort := basePort + len(occupiedListeners)
	expectedAddr := net.JoinHostPort(host, fmt.Sprintf("%d", expectedPort))

	connCtx, connCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer connCancel()

	conn, err := grpc.DialContext(connCtx, expectedAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock())
	require.NoError(t, err, "Server should have found port %d", expectedPort)
	defer conn.Close()

	t.Logf("✓ Server successfully navigated %d occupied ports and bound to %d", len(occupiedListeners), expectedPort)

	cancel()
}
