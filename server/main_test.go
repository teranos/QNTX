package server

import (
	"os"
	"testing"

	"github.com/teranos/QNTX/plugin"
	"go.uber.org/zap"
)

// The node is never without its plugin registry: cmd/qntx sets it before a
// server is made, and these tests set it once the same way.
func TestMain(m *testing.M) {
	plugin.SetDefaultRegistry(plugin.NewRegistry("0.1.0", zap.NewNop().Sugar()))
	os.Exit(m.Run())
}

// bareNode is a node holding nothing but what every node holds.
func bareNode() *QNTXServer {
	return &QNTXServer{pluginRegistry: plugin.GetDefaultRegistry()}
}
