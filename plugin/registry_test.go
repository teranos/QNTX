package plugin

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"go.uber.org/zap"
)

// =============================================================================
// Mock Plugin Implementation
// =============================================================================

type mockPlugin struct {
	metadata       Metadata
	initCalled     bool
	shutdownCalled bool
	initError      error
	shutdownError  error
	healthStatus   HealthStatus
	mu             sync.Mutex
}

func newMockPlugin(name string) *mockPlugin {
	return &mockPlugin{
		metadata: Metadata{
			Name:        name,
			Version:     "1.0.0",
			QNTXVersion: "",
			Description: fmt.Sprintf("Mock %s plugin", name),
			Author:      "Test",
			License:     "MIT",
		},
		healthStatus: HealthStatus{
			Healthy: true,
			Message: "OK",
		},
	}
}

func (m *mockPlugin) Metadata() Metadata {
	return m.metadata
}

func (m *mockPlugin) Initialize(ctx context.Context, services ServiceRegistry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initCalled = true
	return m.initError
}

func (m *mockPlugin) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shutdownCalled = true
	return m.shutdownError
}

func (m *mockPlugin) RegisterHTTP(mux *http.ServeMux) error {
	return nil
}

func (m *mockPlugin) RegisterWebSocket() (map[string]WebSocketHandler, error) {
	return nil, nil
}

func (m *mockPlugin) Health(ctx context.Context) HealthStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.healthStatus
}

// Verify mockPlugin implements DomainPlugin
var _ DomainPlugin = (*mockPlugin)(nil)

// =============================================================================
// Registry Tests
// =============================================================================

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry("1.0.0")
	assert.NotNil(t, registry)
	assert.Equal(t, "1.0.0", registry.version)
	assert.NotNil(t, registry.plugins)
	assert.Empty(t, registry.plugins)
}

func TestRegistry_Register(t *testing.T) {
	t.Run("successful registration", func(t *testing.T) {
		registry := NewRegistry("1.0.0")
		plugin := newMockPlugin("test")

		err := registry.Register(plugin)
		require.NoError(t, err)

		retrieved, ok := registry.Get("test")
		assert.True(t, ok)
		assert.Equal(t, plugin, retrieved)
	})

	t.Run("name conflict", func(t *testing.T) {
		registry := NewRegistry("1.0.0")
		plugin1 := newMockPlugin("test")
		plugin2 := newMockPlugin("test")

		err := registry.Register(plugin1)
		require.NoError(t, err)

		err = registry.Register(plugin2)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
	})

	t.Run("version compatibility - no constraint", func(t *testing.T) {
		registry := NewRegistry("2.5.3")
		plugin := newMockPlugin("test")
		plugin.metadata.QNTXVersion = "" // No constraint

		err := registry.Register(plugin)
		assert.NoError(t, err)
	})

	t.Run("version compatibility - valid constraint", func(t *testing.T) {
		registry := NewRegistry("1.5.0")
		plugin := newMockPlugin("test")
		plugin.metadata.QNTXVersion = "^1.0.0" // 1.x.x compatible

		err := registry.Register(plugin)
		assert.NoError(t, err)
	})

	t.Run("version compatibility - invalid constraint", func(t *testing.T) {
		registry := NewRegistry("2.0.0")
		plugin := newMockPlugin("test")
		plugin.metadata.QNTXVersion = "^1.0.0" // Requires 1.x.x

		err := registry.Register(plugin)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "version incompatible")
	})

	t.Run("invalid version constraint syntax", func(t *testing.T) {
		registry := NewRegistry("1.0.0")
		plugin := newMockPlugin("test")
		plugin.metadata.QNTXVersion = "invalid-constraint"

		err := registry.Register(plugin)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid version constraint")
	})
}

func TestRegistry_Get(t *testing.T) {
	t.Run("existing plugin", func(t *testing.T) {
		registry := NewRegistry("1.0.0")
		plugin := newMockPlugin("test")
		registry.Register(plugin)

		retrieved, ok := registry.Get("test")
		assert.True(t, ok)
		assert.Equal(t, plugin, retrieved)
	})

	t.Run("non-existent plugin", func(t *testing.T) {
		registry := NewRegistry("1.0.0")

		retrieved, ok := registry.Get("nonexistent")
		assert.False(t, ok)
		assert.Nil(t, retrieved)
	})
}

func TestRegistry_List(t *testing.T) {
	t.Run("empty registry", func(t *testing.T) {
		registry := NewRegistry("1.0.0")
		list := registry.List()
		assert.Empty(t, list)
	})

	t.Run("single plugin", func(t *testing.T) {
		registry := NewRegistry("1.0.0")
		registry.Register(newMockPlugin("test"))

		list := registry.List()
		assert.Equal(t, []string{"test"}, list)
	})

	t.Run("multiple plugins - sorted order", func(t *testing.T) {
		registry := NewRegistry("1.0.0")
		registry.Register(newMockPlugin("zebra"))
		registry.Register(newMockPlugin("alpha"))
		registry.Register(newMockPlugin("beta"))

		list := registry.List()
		assert.Equal(t, []string{"alpha", "beta", "zebra"}, list)
		assert.True(t, sort.StringsAreSorted(list), "List should be sorted")
	})
}

func TestRegistry_GetAll(t *testing.T) {
	t.Run("empty registry", func(t *testing.T) {
		registry := NewRegistry("1.0.0")
		all := registry.GetAll()
		assert.Empty(t, all)
	})

	t.Run("multiple plugins", func(t *testing.T) {
		registry := NewRegistry("1.0.0")
		plugin1 := newMockPlugin("test1")
		plugin2 := newMockPlugin("test2")
		registry.Register(plugin1)
		registry.Register(plugin2)

		all := registry.GetAll()
		assert.Len(t, all, 2)
		assert.Equal(t, plugin1, all["test1"])
		assert.Equal(t, plugin2, all["test2"])
	})
}

func TestRegistry_InitializeAll(t *testing.T) {
	t.Run("successful initialization", func(t *testing.T) {
		registry := NewRegistry("1.0.0")
		plugin1 := newMockPlugin("test1")
		plugin2 := newMockPlugin("test2")
		registry.Register(plugin1)
		registry.Register(plugin2)

		mockServices := newMockServiceRegistry()
		err := registry.InitializeAll(context.Background(), mockServices)
		require.NoError(t, err)

		assert.True(t, plugin1.initCalled)
		assert.True(t, plugin2.initCalled)
	})

	t.Run("initialization error", func(t *testing.T) {
		registry := NewRegistry("1.0.0")
		plugin1 := newMockPlugin("test1")
		plugin2 := newMockPlugin("test2")
		plugin1.initError = fmt.Errorf("init failed")
		registry.Register(plugin1)
		registry.Register(plugin2)

		mockServices := newMockServiceRegistry()
		err := registry.InitializeAll(context.Background(), mockServices)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to initialize")
		assert.Contains(t, err.Error(), "test1")
	})

	t.Run("deterministic order", func(t *testing.T) {
		// Verify plugins are initialized in sorted order
		registry := NewRegistry("1.0.0")
		var initOrder []string
		var mu sync.Mutex

		for _, name := range []string{"zebra", "alpha", "beta"} {
			plugin := &trackingPlugin{
				mockPlugin: newMockPlugin(name),
				onInit: func(pluginName string) {
					mu.Lock()
					initOrder = append(initOrder, pluginName)
					mu.Unlock()
				},
			}
			registry.Register(plugin)
		}

		mockServices := newMockServiceRegistry()
		err := registry.InitializeAll(context.Background(), mockServices)
		require.NoError(t, err)

		// Should be initialized in sorted order
		assert.Equal(t, []string{"alpha", "beta", "zebra"}, initOrder)
	})
}

func TestRegistry_ShutdownAll(t *testing.T) {
	t.Run("successful shutdown", func(t *testing.T) {
		registry := NewRegistry("1.0.0")
		plugin1 := newMockPlugin("test1")
		plugin2 := newMockPlugin("test2")
		registry.Register(plugin1)
		registry.Register(plugin2)

		err := registry.ShutdownAll(context.Background())
		require.NoError(t, err)

		assert.True(t, plugin1.shutdownCalled)
		assert.True(t, plugin2.shutdownCalled)
	})

	t.Run("shutdown errors collected", func(t *testing.T) {
		registry := NewRegistry("1.0.0")
		plugin1 := newMockPlugin("test1")
		plugin2 := newMockPlugin("test2")
		plugin1.shutdownError = fmt.Errorf("shutdown failed 1")
		plugin2.shutdownError = fmt.Errorf("shutdown failed 2")
		registry.Register(plugin1)
		registry.Register(plugin2)

		err := registry.ShutdownAll(context.Background())
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "shutdown errors")
	})

	t.Run("reverse order shutdown", func(t *testing.T) {
		// Verify plugins are shut down in reverse order
		registry := NewRegistry("1.0.0")
		var shutdownOrder []string
		var mu sync.Mutex

		for _, name := range []string{"alpha", "beta", "gamma"} {
			plugin := &trackingPlugin{
				mockPlugin: newMockPlugin(name),
				onShutdown: func(pluginName string) {
					mu.Lock()
					shutdownOrder = append(shutdownOrder, pluginName)
					mu.Unlock()
				},
			}
			registry.Register(plugin)
		}

		err := registry.ShutdownAll(context.Background())
		require.NoError(t, err)

		// Should be shut down in reverse order
		assert.Equal(t, []string{"gamma", "beta", "alpha"}, shutdownOrder)
	})
}

func TestRegistry_HealthCheckAll(t *testing.T) {
	t.Run("all healthy", func(t *testing.T) {
		registry := NewRegistry("1.0.0")
		plugin1 := newMockPlugin("test1")
		plugin2 := newMockPlugin("test2")
		registry.Register(plugin1)
		registry.Register(plugin2)

		health := registry.HealthCheckAll(context.Background())
		assert.Len(t, health, 2)
		assert.True(t, health["test1"].Healthy)
		assert.True(t, health["test2"].Healthy)
	})

	t.Run("partial health issues", func(t *testing.T) {
		registry := NewRegistry("1.0.0")
		plugin1 := newMockPlugin("test1")
		plugin2 := newMockPlugin("test2")
		plugin2.healthStatus = HealthStatus{
			Healthy: false,
			Message: "Error",
			Details: map[string]interface{}{"error": "test error"},
		}
		registry.Register(plugin1)
		registry.Register(plugin2)

		health := registry.HealthCheckAll(context.Background())
		assert.Len(t, health, 2)
		assert.True(t, health["test1"].Healthy)
		assert.False(t, health["test2"].Healthy)
		assert.Equal(t, "Error", health["test2"].Message)
	})
}

// =============================================================================
// Version Validation Tests
// =============================================================================

func TestRegistry_validateVersion(t *testing.T) {
	tests := []struct {
		name        string
		qntxVersion string
		constraint  string
		wantErr     bool
	}{
		{
			name:        "no constraint",
			qntxVersion: "1.0.0",
			constraint:  "",
			wantErr:     false,
		},
		{
			name:        "exact match",
			qntxVersion: "1.0.0",
			constraint:  "1.0.0",
			wantErr:     false,
		},
		{
			name:        "caret constraint - compatible",
			qntxVersion: "1.5.2",
			constraint:  "^1.0.0",
			wantErr:     false,
		},
		{
			name:        "caret constraint - incompatible",
			qntxVersion: "2.0.0",
			constraint:  "^1.0.0",
			wantErr:     true,
		},
		{
			name:        "tilde constraint - compatible",
			qntxVersion: "1.2.5",
			constraint:  "~1.2.0",
			wantErr:     false,
		},
		{
			name:        "tilde constraint - incompatible",
			qntxVersion: "1.3.0",
			constraint:  "~1.2.0",
			wantErr:     true,
		},
		{
			name:        "range constraint - compatible",
			qntxVersion: "1.5.0",
			constraint:  ">=1.0.0 <2.0.0",
			wantErr:     false,
		},
		{
			name:        "invalid QNTX version",
			qntxVersion: "invalid",
			constraint:  "^1.0.0",
			wantErr:     true,
		},
		{
			name:        "invalid constraint syntax",
			qntxVersion: "1.0.0",
			constraint:  "not-a-version",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := NewRegistry(tt.qntxVersion)
			metadata := Metadata{
				Name:        "test",
				QNTXVersion: tt.constraint,
			}

			err := registry.validateVersion(metadata)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// =============================================================================
// Global Registry Tests
// =============================================================================

func TestGlobalRegistry(t *testing.T) {
	// Note: Global registry tests need to run in isolation
	// because they modify global state

	t.Run("set and get default registry", func(t *testing.T) {
		// Reset global state
		registryMu.Lock()
		defaultRegistry = nil
		registryMu.Unlock()

		registry := NewRegistry("1.0.0")
		SetDefaultRegistry(registry)

		retrieved := GetDefaultRegistry()
		assert.Equal(t, registry, retrieved)
	})

	t.Run("panic on double initialization", func(t *testing.T) {
		// Reset global state
		registryMu.Lock()
		defaultRegistry = nil
		registryMu.Unlock()

		registry1 := NewRegistry("1.0.0")
		registry2 := NewRegistry("2.0.0")

		SetDefaultRegistry(registry1)
		assert.Panics(t, func() {
			SetDefaultRegistry(registry2)
		})
	})

	t.Run("global Register function", func(t *testing.T) {
		// Reset global state
		registryMu.Lock()
		defaultRegistry = nil
		registryMu.Unlock()

		registry := NewRegistry("1.0.0")
		SetDefaultRegistry(registry)

		plugin := newMockPlugin("test")
		err := Register(plugin)
		assert.NoError(t, err)

		retrieved, ok := Get("test")
		assert.True(t, ok)
		assert.Equal(t, plugin, retrieved)
	})

	t.Run("global functions without registry", func(t *testing.T) {
		// Reset global state
		registryMu.Lock()
		defaultRegistry = nil
		registryMu.Unlock()

		plugin := newMockPlugin("test")
		err := Register(plugin)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not initialized")

		retrieved, ok := Get("test")
		assert.False(t, ok)
		assert.Nil(t, retrieved)

		list := List()
		assert.Nil(t, list)
	})

	t.Run("global List function", func(t *testing.T) {
		// Reset global state
		registryMu.Lock()
		defaultRegistry = nil
		registryMu.Unlock()

		registry := NewRegistry("1.0.0")
		SetDefaultRegistry(registry)

		Register(newMockPlugin("alpha"))
		Register(newMockPlugin("beta"))

		list := List()
		assert.Equal(t, []string{"alpha", "beta"}, list)
	})
}

// =============================================================================
// Concurrency Tests
// =============================================================================

func TestRegistry_Concurrency(t *testing.T) {
	t.Run("concurrent registration", func(t *testing.T) {
		registry := NewRegistry("1.0.0")
		var wg sync.WaitGroup
		const workers = 10

		for i := 0; i < workers; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				plugin := newMockPlugin(fmt.Sprintf("plugin%d", id))
				registry.Register(plugin)
			}(i)
		}

		wg.Wait()
		assert.Len(t, registry.GetAll(), workers)
	})

	t.Run("concurrent read/write", func(t *testing.T) {
		registry := NewRegistry("1.0.0")
		registry.Register(newMockPlugin("test"))

		var wg sync.WaitGroup
		const readers = 5
		const writers = 5

		// Concurrent readers
		for i := 0; i < readers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					registry.Get("test")
					registry.List()
					registry.GetAll()
				}
			}()
		}

		// Concurrent writers
		for i := 0; i < writers; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					plugin := newMockPlugin(fmt.Sprintf("writer%d-%d", id, j))
					registry.Register(plugin)
				}
			}(i)
		}

		wg.Wait()
		// Should not panic or race
	})
}

// =============================================================================
// Mock Service Registry
// =============================================================================

type mockServiceRegistry struct {
	db    *sql.DB
	store ats.AttestationStore
	queue QueueService
}

func newMockServiceRegistry() *mockServiceRegistry {
	return &mockServiceRegistry{
		db:    &sql.DB{},
		store: nil,
		queue: nil,
	}
}

func (m *mockServiceRegistry) Database() *sql.DB                         { return m.db }
func (m *mockServiceRegistry) Logger(domain string) *zap.SugaredLogger   {
	return zap.NewNop().Sugar()
}
func (m *mockServiceRegistry) Config(domain string) Config               { return &mockConfig{} }
func (m *mockServiceRegistry) ATSStore() ats.AttestationStore            { return m.store }
func (m *mockServiceRegistry) Queue() QueueService                       { return m.queue }

// Verify mockServiceRegistry implements ServiceRegistry
var _ ServiceRegistry = (*mockServiceRegistry)(nil)

type mockConfig struct{}

func (m *mockConfig) GetString(key string) string         { return "" }
func (m *mockConfig) GetInt(key string) int                { return 0 }
func (m *mockConfig) GetBool(key string) bool              { return false }
func (m *mockConfig) GetStringSlice(key string) []string   { return nil }
func (m *mockConfig) Get(key string) interface{}           { return nil }
func (m *mockConfig) Set(key string, value interface{})    {}

// Verify mockConfig implements Config
var _ Config = (*mockConfig)(nil)

// =============================================================================
// Tracking Plugin for Order Tests
// =============================================================================

type trackingPlugin struct {
	*mockPlugin
	onInit     func(string)
	onShutdown func(string)
}

func (t *trackingPlugin) Initialize(ctx context.Context, services ServiceRegistry) error {
	if t.onInit != nil {
		t.onInit(t.mockPlugin.metadata.Name)
	}
	return t.mockPlugin.Initialize(ctx, services)
}

func (t *trackingPlugin) Shutdown(ctx context.Context) error {
	if t.onShutdown != nil {
		t.onShutdown(t.mockPlugin.metadata.Name)
	}
	return t.mockPlugin.Shutdown(ctx)
}

// Verify trackingPlugin implements DomainPlugin
var _ DomainPlugin = (*trackingPlugin)(nil)
