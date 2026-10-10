package grpc

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// PluginNamespaceKey is the key of a plugin's record that names the namespace
// the plugin stands in (ADR-046). QNTX's, like the build keys: the plugin does
// not validate it, and the node hands the plugin a token for that namespace.
const PluginNamespaceKey = "namespace"

// PluginTokens is how the node mints a plugin its own store token for the
// namespace its record names. A node that mints none for a namespace says so
// in the error it returns.
type PluginTokens func(plugin, namespace string) (string, error)

// NewConfigProvider creates a ConfigProvider that hands each plugin the config
// its record holds and injects gRPC service endpoints for plugin discovery.
// Pass nil endpoints if no services are available.
func NewConfigProvider(endpoints *ServiceEndpoints, tokens PluginTokens, logger *zap.SugaredLogger) plugin.ConfigProvider {
	return &configProvider{
		endpoints: endpoints,
		tokens:    tokens,
		logger:    logger,
	}
}

// configProvider wraps plugin records with service endpoint injection.
type configProvider struct {
	endpoints *ServiceEndpoints
	tokens    PluginTokens
	logger    *zap.SugaredLogger
}

func (p *configProvider) GetPluginConfig(domain string) plugin.Config {
	return &configWithEndpoints{
		domain:    domain,
		endpoints: p.endpoints,
		tokens:    p.tokens,
		logger:    p.logger,
	}
}

// configWithEndpoints resolves plugin config keys from the plugin's record,
// read when asked so what the plugin element saved is what the next Initialize
// sees, intercepting underscore-prefixed service keys to return gRPC addresses.
type configWithEndpoints struct {
	domain    string
	endpoints *ServiceEndpoints
	tokens    PluginTokens
	logger    *zap.SugaredLogger
	// readErr is every failure to hand the plugin its config, kept for Err.
	readErr error
}

// held is the plugin's config as its record holds it. The interface has no
// error to return, so a record that could not be read, or a plugin with no
// record, is kept once for Err.
func (c *configWithEndpoints) held() map[string]string {
	record, found, err := pluginRecord(c.domain)
	if err != nil && c.readErr == nil {
		c.readErr = errors.Wrapf(err, "plugin %s handed no config: its record was not read", c.domain)
	}
	if !found && c.readErr == nil {
		c.readErr = errors.Newf("plugin %s handed no config: it has no record, so nothing says its config", c.domain)
	}
	return record.Config
}

// Err is every reason the plugin was not handed its config, so Initialize
// fails with it rather than starting the plugin with no config.
func (c *configWithEndpoints) Err() error { return c.readErr }

// failed keeps a failure for Err, the first as the error and each after it
// beside the first.
func (c *configWithEndpoints) failed(err error) {
	if c.readErr == nil {
		c.readErr = err
		return
	}
	c.readErr = errors.WithSecondaryError(c.readErr, err)
}

// authToken is the token the plugin reaches the node's services with: its own,
// for the namespace its record names (ADR-046). A record without the key
// stands nowhere and is handed none, not the shared token on the served store:
// it acts only where a caller acts, through the call's own token. A namespace
// the key names is the node's to mint for or refuse, an empty one included.
func (c *configWithEndpoints) authToken() string {
	raw, named := c.held()[PluginNamespaceKey]
	if !named {
		return ""
	}
	namespace := strings.TrimSpace(raw)
	token, err := c.tokens(c.domain, namespace)
	if err != nil {
		c.failed(errors.Wrapf(err, "plugin %s stands in %q", c.domain, namespace))
		return ""
	}
	return token
}

// unread keeps a value that does not read as the type asked for, for Err.
func (c *configWithEndpoints) unread(key, raw, as string, err error) {
	c.failed(errors.Wrapf(err, "plugin %s config %s value %q does not read as %s", c.domain, key, raw, as))
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

// Set leaves the record as it is, and keeps that for Err: the plugin element
// is what writes a record.
func (c *configWithEndpoints) Set(key string, value any) {
	c.failed(errors.Newf("plugin %s config %s not set to %v: a plugin's config is written in the plugin element", c.domain, key, value))
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
		return c.authToken(), true
	}
	return "", false
}
