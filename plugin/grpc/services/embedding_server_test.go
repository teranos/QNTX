package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/teranos/QNTX/plugin/embedding"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// oneModel is an embedding provider with one model.
type oneModel struct{}

func (oneModel) GenerateEmbedding(string, string) (*embedding.EmbeddingResult, error) {
	return &embedding.EmbeddingResult{Embedding: []float32{1, 0}}, nil
}
func (oneModel) GenerateBatchEmbeddings([]string, string) (*embedding.BatchEmbeddingResult, error) {
	return &embedding.BatchEmbeddingResult{}, nil
}
func (oneModel) GetModelInfo(model string) (*embedding.ModelInfo, error) {
	return &embedding.ModelInfo{Name: "all-MiniLM-L6-v2", Dimensions: 384, MaxSequenceLength: 256}, nil
}

// The router answers which model the provider embeds with: asked with none,
// it is the provider's first, which is what an index of a model is named by.
func TestTheRouterSaysWhichModelTheProviderEmbedsWith(t *testing.T) {
	router := NewEmbeddingServer("token", zap.NewNop().Sugar())

	_, err := router.ModelInfo(context.Background(), &protocol.ModelInfoRequest{AuthToken: "token"})
	require.Error(t, err, "before a provider registers there is no model to say")

	router.SetService(oneModel{})
	info, err := router.ModelInfo(context.Background(), &protocol.ModelInfoRequest{AuthToken: "token"})
	require.NoError(t, err)
	assert.Equal(t, "all-MiniLM-L6-v2", info.GetName())
	assert.Equal(t, int32(384), info.GetDimensions())

	_, err = router.ModelInfo(context.Background(), &protocol.ModelInfoRequest{AuthToken: "another"})
	assert.Error(t, err, "a call without the token handed to plugins is refused")
}
