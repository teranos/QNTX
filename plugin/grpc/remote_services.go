package grpc

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/viper"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/plugin"
	"go.uber.org/zap"
)

// RemoteServiceRegistry provides service access for gRPC plugins.
// gRPC plugins receive this registry with endpoints to connect back to QNTX.
// Services are accessed via gRPC clients that connect to the endpoints.
type RemoteServiceRegistry struct {
	ctx              context.Context      // Parent context for cancellation
	atsStoreEndpoint string
	queueEndpoint    string
	authToken        string
	config           map[string]string
	logger           *zap.SugaredLogger
	atsStoreClient   ats.AttestationStore // Lazy-initialized gRPC client
	queueClient      plugin.QueueService  // Lazy-initialized gRPC client
	pluginRef        plugin.DomainPlugin  // Reference to plugin for metadata lookup
}

// NewRemoteServiceRegistry creates a new remote service registry.
// The provided context is used for all gRPC operations and enables cancellation.
func NewRemoteServiceRegistry(
	ctx context.Context,
	atsStoreEndpoint string,
	queueEndpoint string,
	authToken string,
	config map[string]string,
	logger *zap.SugaredLogger,
	pluginRef plugin.DomainPlugin,
) *RemoteServiceRegistry {
	return &RemoteServiceRegistry{
		ctx:              ctx,
		atsStoreEndpoint: atsStoreEndpoint,
		queueEndpoint:    queueEndpoint,
		authToken:        authToken,
		config:           config,
		logger:           logger,
		pluginRef:        pluginRef,
	}
}

// Database returns nil for remote plugins.
// gRPC plugins do not have direct database access.
// Use ATSStore for attestation operations.
func (r *RemoteServiceRegistry) Database() *sql.DB {
	// gRPC plugins access data via the ATSStore gRPC endpoint
	r.logger.Warn("Database() called on remote plugin - direct DB access not available")
	return nil
}

// Logger returns a logger for the specified domain with version information.
func (r *RemoteServiceRegistry) Logger(domain string) *zap.SugaredLogger {
	// Look up plugin metadata to include version in logger name
	loggerName := domain
	if r.pluginRef != nil {
		metadata := r.pluginRef.Metadata()
		if metadata.Version != "" {
			// Format as: domain v0.4.3
			loggerName = domain + " v" + metadata.Version
		}
	}
	return r.logger.Named(loggerName)
}

// Config returns plugin-specific configuration.
func (r *RemoteServiceRegistry) Config(domain string) plugin.Config {
	return newRemoteConfig(domain, r.config)
}

// ATSStore returns a gRPC client for ATSStore operations.
// The client is lazy-initialized on first access.
func (r *RemoteServiceRegistry) ATSStore() ats.AttestationStore {
	if r.atsStoreClient == nil && r.atsStoreEndpoint != "" {
		client, err := NewRemoteATSStore(r.ctx, r.atsStoreEndpoint, r.authToken, r.logger)
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
		client, err := NewRemoteQueue(r.ctx, r.queueEndpoint, r.authToken, r.logger)
		if err != nil {
			r.logger.Errorw("Failed to create Queue client", "error", err)
			return nil
		}
		r.queueClient = client
	}
	return r.queueClient
}

// remoteConfig provides configuration for remote plugins using viper for parsing.
type remoteConfig struct {
	domain string
	viper  *viper.Viper
}

// newRemoteConfig creates a new remoteConfig with viper backing
func newRemoteConfig(domain string, config map[string]string) *remoteConfig {
	v := viper.New()

	// Load all config values into viper
	for key, value := range config {
		v.Set(key, value)
	}

	return &remoteConfig{
		domain: domain,
		viper:  v,
	}
}

func (c *remoteConfig) GetString(key string) string {
	return c.viper.GetString(key)
}

func (c *remoteConfig) GetInt(key string) int {
	return c.viper.GetInt(key)
}

func (c *remoteConfig) GetBool(key string) bool {
	// First try viper's native bool parsing
	// Viper accepts: 1, t, T, TRUE, true, True, 0, f, F, FALSE, false, False
	if val := c.viper.Get(key); val != nil {
		// Check if it's already a bool
		if b, ok := val.(bool); ok {
			return b
		}

		// If it's a string, check for additional permissive values
		if s, ok := val.(string); ok {
			lower := strings.ToLower(s)
			// Additional permissive values
			if lower == "yes" || lower == "y" || lower == "on" {
				return true
			}
			if lower == "no" || lower == "n" || lower == "off" {
				return false
			}
		}
	}

	// Fall back to viper's GetBool for standard parsing
	return c.viper.GetBool(key)
}

func (c *remoteConfig) GetStringSlice(key string) []string {
	val := c.viper.Get(key)
	if val == nil {
		return nil
	}

	// If it's already a slice, return it
	if slice, ok := val.([]string); ok {
		return slice
	}

	// If it's an interface slice, convert to string slice
	if slice, ok := val.([]interface{}); ok {
		result := make([]string, len(slice))
		for i, v := range slice {
			result[i] = fmt.Sprintf("%v", v)
		}
		return result
	}

	// If it's a string, check if it's JSON array or CSV
	if str, ok := val.(string); ok {
		if str == "" {
			return nil
		}

		// Try parsing as JSON array first
		if strings.HasPrefix(str, "[") {
			var slice []string
			if err := json.Unmarshal([]byte(str), &slice); err == nil {
				return slice
			}
		}

		// Otherwise split by comma
		parts := strings.Split(str, ",")
		// Trim spaces from each part
		for i, part := range parts {
			parts[i] = strings.TrimSpace(part)
		}
		return parts
	}

	return nil
}

func (c *remoteConfig) Get(key string) interface{} {
	return c.viper.Get(key)
}

func (c *remoteConfig) Set(key string, value interface{}) {
	c.viper.Set(key, value)
}

// GetKeys returns all available configuration keys
func (c *remoteConfig) GetKeys() []string {
	keys := c.viper.AllKeys()
	sort.Strings(keys)
	return keys
}
