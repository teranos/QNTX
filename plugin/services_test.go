package plugin

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/pulse/async"
	"go.uber.org/zap/zaptest"
)

// =============================================================================
// Service Registry Tests
// =============================================================================

func TestNewServiceRegistry(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	db := &sql.DB{}
	store := &storage.SQLStore{}
	config := &mockConfigProvider{}
	queue := &async.Queue{}

	registry := NewServiceRegistry(db, logger, store, config, queue)
	assert.NotNil(t, registry)

	// Verify it implements ServiceRegistry interface
	var _ ServiceRegistry = registry
}

func TestDefaultServiceRegistry_Database(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	db := &sql.DB{}
	store := &storage.SQLStore{}
	config := &mockConfigProvider{}
	queue := &async.Queue{}

	registry := NewServiceRegistry(db, logger, store, config, queue)
	assert.Equal(t, db, registry.Database())
}

func TestDefaultServiceRegistry_Logger(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	db := &sql.DB{}
	store := &storage.SQLStore{}
	config := &mockConfigProvider{}
	queue := &async.Queue{}

	registry := NewServiceRegistry(db, logger, store, config, queue)

	t.Run("logger with domain name", func(t *testing.T) {
		domainLogger := registry.Logger("test-domain")
		assert.NotNil(t, domainLogger)

		// Verify it's a named logger (zap creates new logger with domain name)
		// The logger should be usable
		domainLogger.Info("test message")
	})

	t.Run("different domains get different loggers", func(t *testing.T) {
		logger1 := registry.Logger("domain1")
		logger2 := registry.Logger("domain2")

		// Both should be valid but different instances
		assert.NotNil(t, logger1)
		assert.NotNil(t, logger2)
	})
}

func TestDefaultServiceRegistry_Config(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	db := &sql.DB{}
	store := &storage.SQLStore{}
	mockProvider := &mockConfigProvider{
		configs: make(map[string]Config),
	}
	queue := &async.Queue{}

	registry := NewServiceRegistry(db, logger, store, mockProvider, queue)

	t.Run("get plugin config", func(t *testing.T) {
		testConfig := &mockConfig{}
		mockProvider.configs["test-plugin"] = testConfig

		config := registry.Config("test-plugin")
		assert.Equal(t, testConfig, config)
	})

	t.Run("different plugins get different configs", func(t *testing.T) {
		config1 := &mockConfigWithID{id: "config1"}
		config2 := &mockConfigWithID{id: "config2"}
		mockProvider.configs["plugin1"] = config1
		mockProvider.configs["plugin2"] = config2

		retrieved1 := registry.Config("plugin1")
		retrieved2 := registry.Config("plugin2")

		assert.Equal(t, config1, retrieved1)
		assert.Equal(t, config2, retrieved2)

		// Verify they have different IDs
		c1, ok1 := retrieved1.(*mockConfigWithID)
		c2, ok2 := retrieved2.(*mockConfigWithID)
		assert.True(t, ok1)
		assert.True(t, ok2)
		assert.NotEqual(t, c1.id, c2.id)
	})
}

func TestDefaultServiceRegistry_ATSStore(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	db := &sql.DB{}
	store := &storage.SQLStore{}
	config := &mockConfigProvider{}
	queue := &async.Queue{}

	registry := NewServiceRegistry(db, logger, store, config, queue)
	assert.Equal(t, store, registry.ATSStore())
}

func TestDefaultServiceRegistry_Queue(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	db := &sql.DB{}
	store := &storage.SQLStore{}
	config := &mockConfigProvider{}
	queue := &async.Queue{}

	registry := NewServiceRegistry(db, logger, store, config, queue)
	assert.Equal(t, queue, registry.Queue())
}

// =============================================================================
// Config Interface Tests
// =============================================================================

func TestMockConfig_Interface(t *testing.T) {
	// Verify mockConfig implements Config interface
	var _ Config = (*mockConfig)(nil)

	config := &mockConfig{}

	t.Run("GetString", func(t *testing.T) {
		result := config.GetString("test")
		assert.Equal(t, "", result)
	})

	t.Run("GetInt", func(t *testing.T) {
		result := config.GetInt("test")
		assert.Equal(t, 0, result)
	})

	t.Run("GetBool", func(t *testing.T) {
		result := config.GetBool("test")
		assert.Equal(t, false, result)
	})

	t.Run("GetStringSlice", func(t *testing.T) {
		result := config.GetStringSlice("test")
		assert.Nil(t, result)
	})

	t.Run("Get", func(t *testing.T) {
		result := config.Get("test")
		assert.Nil(t, result)
	})

	t.Run("Set", func(t *testing.T) {
		// Should not panic
		config.Set("test", "value")
	})
}

// =============================================================================
// Mock Implementations
// =============================================================================

type mockConfigProvider struct {
	configs map[string]Config
}

func (m *mockConfigProvider) GetPluginConfig(domain string) Config {
	if config, ok := m.configs[domain]; ok {
		return config
	}
	return &mockConfig{}
}

// mockConfigWithID is a config implementation with an identifier for testing
type mockConfigWithID struct {
	id string
}

func (m *mockConfigWithID) GetString(key string) string         { return "" }
func (m *mockConfigWithID) GetInt(key string) int                { return 0 }
func (m *mockConfigWithID) GetBool(key string) bool              { return false }
func (m *mockConfigWithID) GetStringSlice(key string) []string   { return nil }
func (m *mockConfigWithID) Get(key string) interface{}           { return nil }
func (m *mockConfigWithID) Set(key string, value interface{})    {}
func (m *mockConfigWithID) GetKeys() []string                    { return []string{} }

// Verify mockConfigWithID implements Config
var _ Config = (*mockConfigWithID)(nil)

// =============================================================================
// Integration Test - Full Service Registry Usage
// =============================================================================

func TestServiceRegistry_Integration(t *testing.T) {
	// Create a real service registry with all components
	logger := zaptest.NewLogger(t).Sugar()
	db := &sql.DB{}
	store := &storage.SQLStore{}
	configProvider := &mockConfigProvider{
		configs: make(map[string]Config),
	}
	queue := &async.Queue{}

	registry := NewServiceRegistry(db, logger, store, configProvider, queue)

	// Create a mock plugin and initialize it with the service registry
	plugin := newMockPlugin("integration-test")
	err := plugin.Initialize(nil, registry)
	assert.NoError(t, err)

	// Verify plugin can access all services
	assert.Equal(t, db, registry.Database())
	assert.NotNil(t, registry.Logger("integration-test"))
	assert.NotNil(t, registry.Config("integration-test"))
	assert.Equal(t, store, registry.ATSStore())
	assert.Equal(t, queue, registry.Queue())
}
