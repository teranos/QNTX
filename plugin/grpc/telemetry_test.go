package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

var namesSentry = map[string]*protocol.ConfigFieldSchema{
	SentryDSNKey:         {Type: "string"},
	SentryEnvironmentKey: {Type: "string"},
}

var node = NodeTelemetry{DSN: "https://tim@example.com/2", Environment: "production"}

// A plugin that names sentry_dsn is handed the node's DSN and environment.
func TestAPluginNamingSentryIsHandedTheNodes(t *testing.T) {
	config := map[string]string{"namespace": "Clean"}

	handNodeTelemetry(config, namesSentry, node)

	assert.Equal(t, node.DSN, config[SentryDSNKey])
	assert.Equal(t, node.Environment, config[SentryEnvironmentKey])
	assert.Equal(t, "Clean", config["namespace"])
}

// A plugin that does not name sentry_dsn is handed neither key.
func TestAPluginNotNamingSentryIsHandedNothing(t *testing.T) {
	config := map[string]string{}

	handNodeTelemetry(config, map[string]*protocol.ConfigFieldSchema{"namespace": {Type: "string"}}, node)

	assert.Empty(t, config)
}

// A node with no DSN hands nothing: an empty DSN is Sentry off.
func TestANodeWithNoDSNHandsNothing(t *testing.T) {
	config := map[string]string{}

	handNodeTelemetry(config, namesSentry, NodeTelemetry{Environment: "production"})

	assert.Empty(t, config)
}

// What the plugin's record sets is kept.
func TestARecordsSentryKeysAreKept(t *testing.T) {
	config := map[string]string{SentryDSNKey: "https://spike@example.com/9", SentryEnvironmentKey: "laptop"}

	handNodeTelemetry(config, namesSentry, node)

	assert.Equal(t, "https://spike@example.com/9", config[SentryDSNKey])
	assert.Equal(t, "laptop", config[SentryEnvironmentKey])
}

// A node with a DSN and no environment hands the DSN alone.
func TestANodeWithNoEnvironmentHandsTheDSNAlone(t *testing.T) {
	config := map[string]string{}

	handNodeTelemetry(config, namesSentry, NodeTelemetry{DSN: node.DSN})

	assert.Equal(t, node.DSN, config[SentryDSNKey])
	assert.NotContains(t, config, SentryEnvironmentKey)
}
