package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
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

	assert.Equal(t, []string{"https://q.example"}, later.wsConfig.AllowedOrigins)
	assert.Equal(t, DefaultKeepaliveConfig(), later.keepaliveConfig)
}

// Before the node configures WebSockets, a plugin keeps its default.
func TestNothingIsAppliedBeforeTheNodeConfiguresWebSockets(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	manager := NewPluginManager(logger, logger, "")

	early := &ExternalDomainProxy{wsConfig: DefaultWebSocketConfig()}
	manager.applyWebSocket(early)
	assert.Equal(t, DefaultWebSocketConfig(), early.wsConfig)
}
