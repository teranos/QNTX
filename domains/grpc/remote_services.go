package grpc

import (
	"database/sql"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/domains"
	"github.com/teranos/QNTX/pulse/async"
	"go.uber.org/zap"
)

// RemoteServiceRegistry provides service access for external plugins.
// External plugins receive this registry with endpoints to connect back to QNTX.
type RemoteServiceRegistry struct {
	dbEndpoint       string
	atsStoreEndpoint string
	config           map[string]string
	logger           *zap.SugaredLogger
}

// NewRemoteServiceRegistry creates a new remote service registry.
func NewRemoteServiceRegistry(
	dbEndpoint string,
	atsStoreEndpoint string,
	config map[string]string,
	logger *zap.SugaredLogger,
) *RemoteServiceRegistry {
	return &RemoteServiceRegistry{
		dbEndpoint:       dbEndpoint,
		atsStoreEndpoint: atsStoreEndpoint,
		config:           config,
		logger:           logger,
	}
}

// Database returns nil for remote plugins.
// External plugins should not have direct database access.
// Use ATSStore for attestation operations.
func (r *RemoteServiceRegistry) Database() *sql.DB {
	// External plugins don't have direct database access
	// They communicate via the ATSStore gRPC endpoint
	r.logger.Warn("Database() called on remote plugin - direct DB access not available")
	return nil
}

// Logger returns a logger for the specified domain.
func (r *RemoteServiceRegistry) Logger(domain string) *zap.SugaredLogger {
	return r.logger.Named(domain)
}

// Config returns plugin-specific configuration.
func (r *RemoteServiceRegistry) Config(domain string) domains.Config {
	return &remoteConfig{
		domain: domain,
		config: r.config,
	}
}

// ATSStore returns nil - external plugins use gRPC for attestation operations.
func (r *RemoteServiceRegistry) ATSStore() *storage.SQLStore {
	// External plugins should use the gRPC attestation service
	r.logger.Warn("ATSStore() called on remote plugin - use gRPC attestation service")
	return nil
}

// Queue returns nil - external plugins use gRPC for queue operations.
func (r *RemoteServiceRegistry) Queue() *async.Queue {
	// External plugins should use the gRPC queue service
	r.logger.Warn("Queue() called on remote plugin - use gRPC queue service")
	return nil
}

// remoteConfig provides configuration for remote plugins.
type remoteConfig struct {
	domain string
	config map[string]string
}

func (c *remoteConfig) GetString(key string) string {
	return c.config[key]
}

func (c *remoteConfig) GetInt(key string) int {
	// TODO: Implement proper integer parsing
	return 0
}

func (c *remoteConfig) GetBool(key string) bool {
	val := c.config[key]
	return val == "true" || val == "1"
}

func (c *remoteConfig) GetStringSlice(key string) []string {
	// TODO: Implement proper slice parsing
	return nil
}

func (c *remoteConfig) Get(key string) interface{} {
	return c.config[key]
}

func (c *remoteConfig) Set(key string, value interface{}) {
	if s, ok := value.(string); ok {
		c.config[key] = s
	}
}
