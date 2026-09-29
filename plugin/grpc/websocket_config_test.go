package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// A plugin enabled in the plugin element after the node configured WebSockets
// gets the same settings as one loaded at boot.
func TestAPluginLoadedLaterGetsTheNodesWebSocketSettings(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	manager := NewPluginManager(logger, logger, "")
	manager.ConfigureWebSocket(DefaultKeepaliveConfig(), WebSocketConfig{AllowedOrigins: []string{"https://q.example"}})

	later := &ExternalDomainProxy{}
	manager.applyWebSocket(later)

	require.NotNil(t, later.wsConfig)
	assert.Equal(t, []string{"https://q.example"}, later.wsConfig.AllowedOrigins)
	require.NotNil(t, later.keepaliveConfig)
}

// Before the node configures WebSockets, a plugin keeps its default.
func TestNothingIsAppliedBeforeTheNodeConfiguresWebSockets(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	manager := NewPluginManager(logger, logger, "")

	early := &ExternalDomainProxy{}
	manager.applyWebSocket(early)
	assert.Nil(t, early.wsConfig)
}
