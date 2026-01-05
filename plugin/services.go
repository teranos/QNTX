package plugin

import (
	"database/sql"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/pulse/async"
	"go.uber.org/zap"
)

// QueueService defines the job queue operations available to plugins.
// This interface allows both local and remote queue implementations.
type QueueService interface {
	// Enqueue adds a new job to the queue
	Enqueue(job *async.Job) error

	// GetJob retrieves a job by ID
	GetJob(id string) (*async.Job, error)

	// UpdateJob updates a job's state
	UpdateJob(job *async.Job) error

	// ListJobs lists jobs with optional status filter
	ListJobs(status *async.JobStatus, limit int) ([]*async.Job, error)
}

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
	ATSStore() ats.AttestationStore

	// Queue returns the Pulse async job queue
	Queue() QueueService
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
	store  ats.AttestationStore
	config ConfigProvider
	queue  QueueService
}

// ConfigProvider provides configuration for plugins
type ConfigProvider interface {
	// GetPluginConfig returns configuration for a specific plugin
	GetPluginConfig(domain string) Config
}

// NewServiceRegistry creates a new service registry
func NewServiceRegistry(db *sql.DB, logger *zap.SugaredLogger, store ats.AttestationStore, config ConfigProvider, queue QueueService) ServiceRegistry {
	return &DefaultServiceRegistry{
		db:     db,
		logger: logger,
		store:  store,
		config: config,
		queue:  queue,
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
func (r *DefaultServiceRegistry) ATSStore() ats.AttestationStore {
	return r.store
}

// Queue returns the Pulse async job queue
func (r *DefaultServiceRegistry) Queue() QueueService {
	return r.queue
}
