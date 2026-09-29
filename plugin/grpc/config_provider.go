package grpc

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/teranos/QNTX/plugin"
)

// NewConfigProvider creates a ConfigProvider that hands each plugin the config
// its record holds and injects gRPC service endpoints for plugin discovery.
// Pass nil endpoints if no services are available.
func NewConfigProvider(endpoints *ServiceEndpoints) plugin.ConfigProvider {
	return &configProvider{
		endpoints: endpoints,
	}
}

// configProvider wraps am config with service endpoint injection.
type configProvider struct {
	endpoints *ServiceEndpoints
}

func (p *configProvider) GetPluginConfig(domain string) plugin.Config {
	return &configWithEndpoints{
		domain:    domain,
		endpoints: p.endpoints,
	}
}

// configWithEndpoints resolves plugin config keys from the plugin's record,
// read when asked so what the plugin element saved is what the next Initialize
// sees, intercepting underscore-prefixed service keys to return gRPC addresses.
type configWithEndpoints struct {
	domain    string
	endpoints *ServiceEndpoints
}

// held is the plugin's config as its record holds it.
func (c *configWithEndpoints) held() map[string]string {
	record, _ := pluginRecord(c.domain)
	return record.Config
}

func (c *configWithEndpoints) GetString(key string) string {
	if v, ok := c.endpointValue(key); ok {
		return v
	}
	return c.held()[key]
}

func (c *configWithEndpoints) GetInt(key string) int {
	n, err := strconv.Atoi(strings.TrimSpace(c.held()[key]))
	if err != nil {
		return 0
	}
	return n
}

func (c *configWithEndpoints) GetBool(key string) bool {
	b, err := strconv.ParseBool(strings.TrimSpace(c.held()[key]))
	return err == nil && b
}

// GetStringSlice reads a JSON list, which is how the plugin element writes one.
func (c *configWithEndpoints) GetStringSlice(key string) []string {
	var list []string
	if err := json.Unmarshal([]byte(c.held()[key]), &list); err != nil {
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

// Set leaves the record as it is: the plugin element is what writes a record.
func (c *configWithEndpoints) Set(string, any) {}

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
