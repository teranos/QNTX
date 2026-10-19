package server

import (
	"os"
	"testing"

	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/server/statusline"
	"go.uber.org/zap"
)

// The node is never without its plugin registry: cmd/qntx sets it before a
// server is made, and these tests set it once the same way. Nor is it without
// a port, an address to listen on and a store, which a node is told in its environment
// as well as in am.toml.
func TestMain(m *testing.M) {
	plugin.SetDefaultRegistry(plugin.NewRegistry("0.1.0", zap.NewNop().Sugar()))
	for key, value := range map[string]string{
		"QNTX_SERVER_PORT":         "8770",
		"QNTX_SERVER_BIND_ADDRESS": "127.0.0.1",
		"QNTX_STORAGE_BACKEND":     "sqlite",
	} {
		if err := os.Setenv(key, value); err != nil {
			panic("the test node's " + key + " could not be said: " + err.Error())
		}
	}
	os.Exit(m.Run())
}

// bareNode is a node holding nothing but what every node holds.
func bareNode() *QNTXServer {
	return &QNTXServer{pluginRegistry: plugin.GetDefaultRegistry(), news: statusline.NewNewsLog(), handlerFailures: statusline.NewHandlerFailureLog()}
}
