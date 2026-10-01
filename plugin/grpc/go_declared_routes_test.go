package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	pluginpkg "github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap/zaptest"
	"google.golang.org/protobuf/proto"
)

// declaringGoPlugin is a Go plugin that declares its routes and nothing else.
type declaringGoPlugin struct {
	*mockPlugin
	routes []*protocol.RouteInfo
}

func (p *declaringGoPlugin) DeclaredRoutes() []*protocol.RouteInfo { return p.routes }

// "the plugin should just be able to declare routes"
func TestPluginServer_HandsOnTheRoutesAGoPluginDeclares(t *testing.T) {
	declared := []*protocol.RouteInfo{
		{Method: "POST", Path: "/identity", Description: "as <email> is mail:address of <user_id>"},
	}
	server := NewPluginServer(&declaringGoPlugin{mockPlugin: newMockPlugin(), routes: declared}, zaptest.NewLogger(t).Sugar())

	resp, err := server.Initialize(context.Background(), &protocol.InitializeRequest{})
	require.NoError(t, err)

	require.Len(t, resp.HttpRoutes, len(declared))
	for i := range declared {
		assert.True(t, proto.Equal(declared[i], resp.HttpRoutes[i]), "route %d: declared %v, handed %v", i, declared[i], resp.HttpRoutes[i])
	}
}

// storeDialingPlugin reads where the ATS store is, to dial it with a call's token.
type storeDialingPlugin struct {
	*mockPlugin
	endpoint string
}

func (p *storeDialingPlugin) Initialize(_ context.Context, services pluginpkg.ServiceRegistry) error {
	p.endpoint = services.Config(p.Metadata().Name).GetString("_ats_store_endpoint")
	return nil
}

// A plugin answering a sigil writes with the token the node handed it for that
// call, so it has to know where the store is to present it.
func TestPluginServer_TellsAGoPluginWhereTheStoreIs(t *testing.T) {
	p := &storeDialingPlugin{mockPlugin: newMockPlugin()}
	server := NewPluginServer(p, zaptest.NewLogger(t).Sugar())

	_, err := server.Initialize(context.Background(), &protocol.InitializeRequest{AtsStoreEndpoint: "localhost:50051"})
	require.NoError(t, err)

	assert.Equal(t, "localhost:50051", p.endpoint)
}
