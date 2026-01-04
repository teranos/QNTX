package domains

import (
	"database/sql"

	"github.com/teranos/QNTX/ats/storage"
	"go.uber.org/zap"
)

// ServiceRegistry provides access to QNTX core services for domain plugins.
// Plugins use this registry to look up services they need.
type ServiceRegistry interface {
	// Database returns the shared QNTX database connection
	Database() *sql.DB

	// Logger returns a logger for this plugin
	Logger(domain string) *zap.SugaredLogger

	// Config returns plugin-specific configuration
	Config(domain string) Config

	// ATSStore returns the attestation storage interface
	ATSStore() *storage.SQLStore
}

// Config provides access to plugin configuration
type Config interface {
	// GetString retrieves a string configuration value
	GetString(key string) string

	// GetInt retrieves an integer configuration value
	GetInt(key string) int

	// GetBool retrieves a boolean configuration value
	GetBool(key string) bool

	// GetStringSlice retrieves a string slice configuration value
	GetStringSlice(key string) []string

	// Get retrieves a raw configuration value
	Get(key string) interface{}

	// Set sets a configuration value (for runtime overrides)
	Set(key string, value interface{})
}

// DefaultServiceRegistry is the standard implementation of ServiceRegistry
type DefaultServiceRegistry struct {
	db     *sql.DB
	logger *zap.SugaredLogger
	store  *storage.SQLStore
	config ConfigProvider
}

// ConfigProvider provides configuration for plugins
type ConfigProvider interface {
	// GetPluginConfig returns configuration for a specific plugin
	GetPluginConfig(domain string) Config
}

// NewServiceRegistry creates a new service registry
func NewServiceRegistry(db *sql.DB, logger *zap.SugaredLogger, store *storage.SQLStore, config ConfigProvider) ServiceRegistry {
	return &DefaultServiceRegistry{
		db:     db,
		logger: logger,
		store:  store,
		config: config,
	}
}

// Database returns the shared QNTX database connection
func (r *DefaultServiceRegistry) Database() *sql.DB {
	return r.db
}

// Logger returns a logger for the specified domain
func (r *DefaultServiceRegistry) Logger(domain string) *zap.SugaredLogger {
	return r.logger.Named(domain)
}

// Config returns plugin-specific configuration
func (r *DefaultServiceRegistry) Config(domain string) Config {
	return r.config.GetPluginConfig(domain)
}

// ATSStore returns the attestation storage interface
func (r *DefaultServiceRegistry) ATSStore() *storage.SQLStore {
	return r.store
}
