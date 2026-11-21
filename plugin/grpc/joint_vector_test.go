package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/teranos/QNTX/plugin/grpc/services"
)

// A vector search provider enabled or restarted at runtime is registered with
// the router, and what asked to be told of it is told.
func TestAVectorSearchProviderJoinedAtRuntimeIsRegistered(t *testing.T) {
	logger := zap.NewNop().Sugar()
	sm := &ServicesManager{logger: logger, vectorSearchRouter: services.NewVectorSearchServer("token", logger)}
	m := &PluginManager{logger: logger}
	var told []string
	m.SetOnVectorSearchProviderReady(func(name string) { told = append(told, name) })

	roles, err := servicesJoint{m: m, sm: sm}.join("qdrant", &ExternalDomainProxy{vectorSearchProvider: true})
	require.NoError(t, err)
	assert.Contains(t, roles, "vector-search-provider")
	assert.True(t, sm.vectorSearchRouter.HasProvider())
	assert.Equal(t, []string{"qdrant"}, told)
}
