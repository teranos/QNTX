package grpc

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/teranos/QNTX/plugin"
	"go.uber.org/zap"
)

// NewConfigProvider creates a ConfigProvider that hands each plugin the config
// its record holds and injects gRPC service endpoints for plugin discovery.
// Pass nil endpoints if no services are available.
func NewConfigProvider(endpoints *ServiceEndpoints, logger *zap.SugaredLogger) plugin.ConfigProvider {
	return &configProvider{
		endpoints: endpoints,
		logger:    logger,
	}
}

// configProvider wraps plugin records with service endpoint injection.
type configProvider struct {
	endpoints *ServiceEndpoints
	logger    *zap.SugaredLogger
}

func (p *configProvider) GetPluginConfig(domain string) plugin.Config {
	return &configWithEndpoints{
		domain:    domain,
		endpoints: p.endpoints,
		logger:    p.logger,
	}
}

// configWithEndpoints resolves plugin config keys from the plugin's record,
// read when asked so what the plugin element saved is what the next Initialize
// sees, intercepting underscore-prefixed service keys to return gRPC addresses.
type configWithEndpoints struct {
	domain    string
	endpoints *ServiceEndpoints
	logger    *zap.SugaredLogger
	// readErr is the first failure to read the plugin's record, kept for Err.
	readErr error
}

// held is the plugin's config as its record holds it. The interface has no
// error to return, so a record that could not be read is said once and kept.
func (c *configWithEndpoints) held() map[string]string {
	record, _, err := pluginRecord(c.domain)
	if err != nil && c.readErr == nil {
		c.readErr = err
		c.logger.Errorw("Plugin handed no config: its record was not read", "plugin", c.domain, "error", err)
	}
	return record.Config
}

// Err is why the plugin's record could not be read, so Initialize fails with
// it rather than starting the plugin with no config.
func (c *configWithEndpoints) Err() error { return c.readErr }

// unread says a value that does not read as the type asked for.
func (c *configWithEndpoints) unread(key, raw, as string, err error) {
	c.logger.Errorw("Plugin config value does not read as "+as,
		"plugin", c.domain, "key", key, "value", raw, "error", err)
}

func (c *configWithEndpoints) GetString(key string) string {
	if v, ok := c.endpointValue(key); ok {
		return v
	}
	return c.held()[key]
}

func (c *configWithEndpoints) GetInt(key string) int {
	raw, set := c.held()[key]
	if !set {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		c.unread(key, raw, "an integer", err)
		return 0
	}
	return n
}

func (c *configWithEndpoints) GetBool(key string) bool {
	raw, set := c.held()[key]
	if !set {
		return false
	}
	b, err := strconv.ParseBool(strings.TrimSpace(raw))
	if err != nil {
		c.unread(key, raw, "true or false", err)
		return false
	}
	return b
}

// GetStringSlice reads a JSON list, which is how the plugin element writes one.
func (c *configWithEndpoints) GetStringSlice(key string) []string {
	raw, set := c.held()[key]
	if !set {
		return nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		c.unread(key, raw, "a JSON list of strings", err)
		return nil
	}
	return list
}

func (c *configWithEndpoints) Get(key string) any {
	if v, ok := c.endpointValue(key); ok {
		return v
	}
	if v, found := c.held()[key]; found {
		return v
	}
	return nil
}

// Set leaves the record as it is, and says so: the plugin element is what
// writes a record.
func (c *configWithEndpoints) Set(key string, value any) {
	c.logger.Errorw("Plugin config not set: a plugin's config is written in the plugin element",
		"plugin", c.domain, "key", key, "value", value)
}

func (c *configWithEndpoints) GetKeys() []string {
	held := c.held()
	keys := make([]string, 0, len(held))
	for key := range held {
		keys = append(keys, key)
	}
	return keys
}

// endpointValue returns a service endpoint value for underscore-prefixed keys.
func (c *configWithEndpoints) endpointValue(key string) (string, bool) {
	if c.endpoints == nil {
		return "", false
	}
	switch key {
	case "_ats_store_endpoint":
		return c.endpoints.ATSStoreAddress, true
	case "_queue_endpoint":
		return c.endpoints.QueueAddress, true
	case "_schedule_endpoint":
		return c.endpoints.ScheduleAddress, true
	case "_file_service_endpoint":
		return c.endpoints.FileServiceAddress, true
	case "_llm_endpoint":
		return c.endpoints.LLMAddress, true
	case "_embedding_endpoint":
		return c.endpoints.EmbeddingAddress, true
	case "_vector_search_endpoint":
		return c.endpoints.VectorSearchAddress, true
	case "_ground_endpoint":
		return c.endpoints.GroundAddress, true
	case "_search_endpoint":
		return c.endpoints.SearchAddress, true
	case "_fetch_endpoint":
		return c.endpoints.FetchAddress, true
	case "_mail_endpoint":
		return c.endpoints.MailAddress, true
	case "_auth_token":
		return c.endpoints.AuthToken, true
	}
	return "", false
}
