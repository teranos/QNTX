package grpc

import (
	"strings"

	appcfg "github.com/teranos/QNTX/am"
	"github.com/teranos/QNTX/plugin"
)

// NewConfigProvider creates a ConfigProvider that reads from am.toml
// and injects gRPC service endpoints for plugin discovery.
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

// configWithEndpoints resolves plugin config keys from am.toml,
// intercepting underscore-prefixed service keys to return gRPC addresses.
type configWithEndpoints struct {
	domain    string
	endpoints *ServiceEndpoints
}

func (c *configWithEndpoints) GetString(key string) string {
	if v, ok := c.endpointValue(key); ok {
		return v
	}
	return appcfg.GetString(c.domain + "." + key)
}

func (c *configWithEndpoints) GetInt(key string) int {
	return appcfg.GetInt(c.domain + "." + key)
}

func (c *configWithEndpoints) GetBool(key string) bool {
	return appcfg.GetBool(c.domain + "." + key)
}

func (c *configWithEndpoints) GetStringSlice(key string) []string {
	return appcfg.GetStringSlice(c.domain + "." + key)
}

func (c *configWithEndpoints) Get(key string) any {
	if v, ok := c.endpointValue(key); ok {
		return v
	}
	return appcfg.Get(c.domain + "." + key)
}

func (c *configWithEndpoints) Set(key string, value any) {
	appcfg.Set(c.domain+"."+key, value)
}

func (c *configWithEndpoints) GetKeys() []string {
	v := appcfg.GetViper()
	if v == nil {
		return []string{}
	}

	allKeys := v.AllKeys()
	prefix := c.domain + "."
	var keys []string

	for _, key := range allKeys {
		if after, ok := strings.CutPrefix(key, prefix); ok {
			keys = append(keys, after)
		}
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
	case "_auth_token":
		return c.endpoints.AuthToken, true
	}
	return "", false
}
