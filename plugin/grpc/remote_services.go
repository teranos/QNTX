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
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// RemoteServiceRegistry provides service access for gRPC plugins.
// gRPC plugins receive this registry with endpoints to connect back to QNTX.
// Services are accessed via gRPC clients that connect to the endpoints.
//
// Each client is made when the registry is, for the endpoint the node handed.
// A gRPC client connects when first called, so a service the node is not
// running is a client whose calls fail, saying so, not a nil to ask about.
type RemoteServiceRegistry struct {
	config             map[string]string
	logger             *zap.SugaredLogger
	atsStoreClient     *RemoteATSStore
	queueClient        *RemoteQueue
	scheduleClient     *RemoteSchedule
	fileServiceClient  *RemoteFileService
	llmClient          *RemoteLLM
	vectorSearchClient *RemoteVectorSearch
	searchClient       *RemoteSearch
	pluginRef          plugin.DomainPlugin // Reference to plugin for metadata lookup
}

// NewRemoteServiceRegistry creates a new remote service registry.
// Uses background context for gRPC clients — the caller's context (typically
// the Initialize RPC) is short-lived and cancelled when the RPC returns,
// but ATSStore/Queue clients must outlive initialization.
func NewRemoteServiceRegistry(
	ctx context.Context,
	atsStoreEndpoint string,
	queueEndpoint string,
	scheduleEndpoint string,
	fileServiceEndpoint string,
	llmEndpoint string,
	vectorSearchEndpoint string,
	searchEndpoint string,
	authToken string,
	config map[string]string,
	logger *zap.SugaredLogger,
	pluginRef plugin.DomainPlugin,
) (*RemoteServiceRegistry, error) {
	outlives := context.Background()
	r := &RemoteServiceRegistry{config: config, logger: logger, pluginRef: pluginRef}
	var err error
	if r.atsStoreClient, err = NewRemoteATSStore(outlives, atsStoreEndpoint, authToken, logger); err != nil {
		return nil, errors.Wrapf(err, "no ATSStore client for %q", atsStoreEndpoint)
	}
	if r.queueClient, err = NewRemoteQueue(outlives, queueEndpoint, authToken, logger); err != nil {
		return nil, errors.Wrapf(err, "no Queue client for %q", queueEndpoint)
	}
	if r.scheduleClient, err = NewRemoteSchedule(outlives, scheduleEndpoint, authToken, logger); err != nil {
		return nil, errors.Wrapf(err, "no Schedule client for %q", scheduleEndpoint)
	}
	if r.fileServiceClient, err = NewRemoteFileService(outlives, fileServiceEndpoint, authToken, logger); err != nil {
		return nil, errors.Wrapf(err, "no FileService client for %q", fileServiceEndpoint)
	}
	if r.llmClient, err = NewRemoteLLM(outlives, llmEndpoint, logger); err != nil {
		return nil, errors.Wrapf(err, "no LLM client for %q", llmEndpoint)
	}
	if r.vectorSearchClient, err = NewRemoteVectorSearch(outlives, vectorSearchEndpoint, authToken, logger); err != nil {
		return nil, errors.Wrapf(err, "no VectorSearch client for %q", vectorSearchEndpoint)
	}
	if r.searchClient, err = NewRemoteSearch(outlives, searchEndpoint, logger); err != nil {
		return nil, errors.Wrapf(err, "no Search client for %q", searchEndpoint)
	}
	return r, nil
}

// Database returns nil for remote plugins.
// gRPC plugins do not have direct database access.
// Use ATSStore for attestation operations.
func (r *RemoteServiceRegistry) Database() *sql.DB {
	// gRPC plugins access data via the ATSStore gRPC endpoint
	r.logger.Warn("Database() called on remote plugin - direct DB access not available")
	return nil
}

// Logger returns a logger for the specified domain with version information,
// named as: domain v0.4.3
func (r *RemoteServiceRegistry) Logger(domain string) *zap.SugaredLogger {
	return r.logger.Named(domain + " v" + r.pluginRef.Metadata().Version)
}

// Config returns plugin-specific configuration.
func (r *RemoteServiceRegistry) Config(domain string) plugin.Config {
	return newRemoteConfig(domain, r.config)
}

// ATSStore returns a gRPC client for ATSStore operations.
func (r *RemoteServiceRegistry) ATSStore() ats.AttestationStore {
	return r.atsStoreClient
}

// Queue returns a gRPC client for Queue operations.
func (r *RemoteServiceRegistry) Queue() plugin.QueueService {
	return r.queueClient
}

// Schedule returns a gRPC client for Schedule operations.
func (r *RemoteServiceRegistry) Schedule() plugin.ScheduleService {
	return r.scheduleClient
}

// FileService returns a gRPC client for file operations.
func (r *RemoteServiceRegistry) FileService() plugin.FileService {
	return r.fileServiceClient
}

// LLM returns a gRPC client for LLM operations.
func (r *RemoteServiceRegistry) LLM() plugin.LLMService {
	return r.llmClient
}

// VectorSearch returns a gRPC client for vector search operations.
func (r *RemoteServiceRegistry) VectorSearch() plugin.VectorSearchService {
	return r.vectorSearchClient
}

// Search returns a gRPC client for Search operations.
func (r *RemoteServiceRegistry) Search() plugin.SearchService {
	return r.searchClient
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

// GetBool reads yes, y and on as true, no, n and off as false, and anything
// else as viper parses a bool: 1, t, T, TRUE, true, True, 0, f, F, FALSE,
// false, False.
func (c *remoteConfig) GetBool(key string) bool {
	switch strings.ToLower(c.viper.GetString(key)) {
	case "yes", "y", "on":
		return true
	case "no", "n", "off":
		return false
	}
	return c.viper.GetBool(key)
}

// GetStringSlice reads a list held as a list, or a string holding a JSON
// array or comma separated values. Anything else is as viper casts it.
func (c *remoteConfig) GetStringSlice(key string) []string {
	switch val := c.viper.Get(key).(type) {
	case []string:
		return val
	case []any:
		result := make([]string, len(val))
		for i, v := range val {
			result[i] = fmt.Sprintf("%v", v)
		}
		return result
	case string:
		return listIn(val)
	}
	return c.viper.GetStringSlice(key)
}

// listIn is the list a string holds: a JSON array, or values separated by
// commas, each trimmed. A string holding no value holds no list.
func listIn(str string) []string {
	if strings.HasPrefix(str, "[") {
		var slice []string
		if err := json.Unmarshal([]byte(str), &slice); err == nil {
			return slice
		}
	}
	parts := strings.FieldsFunc(str, func(r rune) bool { return r == ',' })
	for i, part := range parts {
		parts[i] = strings.TrimSpace(part)
	}
	return parts
}

func (c *remoteConfig) Get(key string) any {
	return c.viper.Get(key)
}

func (c *remoteConfig) Set(key string, value any) {
	c.viper.Set(key, value)
}

// GetKeys returns all available configuration keys
func (c *remoteConfig) GetKeys() []string {
	keys := c.viper.AllKeys()
	sort.Strings(keys)
	return keys
}
