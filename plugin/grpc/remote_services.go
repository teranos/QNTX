package grpc

import (
	"database/sql"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/plugin"
	"go.uber.org/zap"
)

// RemoteServiceRegistry provides service access for external plugins.
// External plugins receive this registry with endpoints to connect back to QNTX.
// Services are accessed via gRPC clients that connect to the endpoints.
type RemoteServiceRegistry struct {
	atsStoreEndpoint string
	queueEndpoint    string
	authToken        string
	config           map[string]string
	logger           *zap.SugaredLogger
	atsStoreClient   ats.AttestationStore // Lazy-initialized gRPC client
	queueClient      plugin.QueueService  // Lazy-initialized gRPC client
}

// NewRemoteServiceRegistry creates a new remote service registry.
func NewRemoteServiceRegistry(
	atsStoreEndpoint string,
	queueEndpoint string,
	authToken string,
	config map[string]string,
	logger *zap.SugaredLogger,
) *RemoteServiceRegistry {
	return &RemoteServiceRegistry{
		atsStoreEndpoint: atsStoreEndpoint,
		queueEndpoint:    queueEndpoint,
		authToken:        authToken,
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
func (r *RemoteServiceRegistry) Config(domain string) plugin.Config {
	return &remoteConfig{
		domain: domain,
		config: r.config,
	}
}

// ATSStore returns a gRPC client for ATSStore operations.
// The client is lazy-initialized on first access.
func (r *RemoteServiceRegistry) ATSStore() ats.AttestationStore {
	if r.atsStoreClient == nil && r.atsStoreEndpoint != "" {
		client, err := NewRemoteATSStore(r.atsStoreEndpoint, r.authToken, r.logger)
		if err != nil {
			r.logger.Errorw("Failed to create ATSStore client", "error", err)
			return nil
		}
		r.atsStoreClient = client
	}
	return r.atsStoreClient
}

// Queue returns a gRPC client for Queue operations.
// The client is lazy-initialized on first access.
func (r *RemoteServiceRegistry) Queue() plugin.QueueService {
	if r.queueClient == nil && r.queueEndpoint != "" {
		client, err := NewRemoteQueue(r.queueEndpoint, r.authToken, r.logger)
		if err != nil {
			r.logger.Errorw("Failed to create Queue client", "error", err)
			return nil
		}
		r.queueClient = client
	}
	return r.queueClient
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
	val, ok := c.config[key]
	if !ok {
		return 0
	}
	// Try to parse as int
	if intVal, err := strconv.Atoi(val); err == nil {
		return intVal
	}
	// Try to parse as float and convert
	if floatVal, err := strconv.ParseFloat(val, 64); err == nil {
		return int(floatVal)
	}
	return 0
}

func (c *remoteConfig) GetBool(key string) bool {
	val := c.config[key]
	return val == "true" || val == "1"
}

func (c *remoteConfig) GetStringSlice(key string) []string {
	val, ok := c.config[key]
	if !ok {
		return nil
	}
	// Try to parse as JSON array
	var slice []string
	if err := json.Unmarshal([]byte(val), &slice); err == nil {
		return slice
	}
	// If not JSON, treat as comma-separated
	return strings.Split(val, ",")
}

func (c *remoteConfig) Get(key string) interface{} {
	return c.config[key]
}

func (c *remoteConfig) Set(key string, value interface{}) {
	if s, ok := value.(string); ok {
		c.config[key] = s
	}
}
