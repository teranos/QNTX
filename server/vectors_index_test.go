package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
)

// freshProvider is a vector search provider that holds no index until asked
// to make one, and refuses a vector for an index it was not asked to make.
type freshProvider struct {
	made map[string]bool
}

func (p *freshProvider) CreateIndex(_ context.Context, in *protocol.CreateIndexRequest, _ ...grpc.CallOption) (*protocol.CreateIndexResponse, error) {
	p.made[in.GetName()] = true
	return &protocol.CreateIndexResponse{Name: in.GetName()}, nil
}

func (p *freshProvider) AddVectors(_ context.Context, in *protocol.AddVectorsRequest, _ ...grpc.CallOption) (*protocol.AddVectorsResponse, error) {
	if !p.made[in.GetIndex()] {
		return nil, assert.AnError
	}
	return &protocol.AddVectorsResponse{Added: int32(len(in.GetVectors()))}, nil
}

func (p *freshProvider) DeleteVectors(context.Context, *protocol.DeleteVectorsRequest, ...grpc.CallOption) (*protocol.DeleteVectorsResponse, error) {
	return &protocol.DeleteVectorsResponse{}, nil
}

func (p *freshProvider) Search(context.Context, *protocol.VectorSearchRequest, ...grpc.CallOption) (*protocol.VectorSearchResponse, error) {
	return &protocol.VectorSearchResponse{}, nil
}

// A provider that registers in place of another is asked to make the index
// again before vectors are added to it.
func TestAReplacementProviderIsAskedToMakeTheIndexAgain(t *testing.T) {
	router := services.NewVectorSearchServer("token", zap.NewNop().Sugar())
	index := &routedIndex{router: func() (*services.VectorSearchServer, string, *protocol.Refusal) {
		return router, "token", nil
	}}

	first := &freshProvider{made: map[string]bool{}}
	router.SetService(first)
	require.NoError(t, index.Keep("m", 2, "a", []float32{1, 0}))

	second := &freshProvider{made: map[string]bool{}}
	router.SetService(second)
	index.providerChanged()
	require.NoError(t, index.Keep("m", 2, "a", []float32{1, 0}), "the new provider is asked to make embeddings.m")
	assert.True(t, second.made["embeddings.m"])
}

// replacedMidCall is a provider replaced while its CreateIndex is answering.
type replacedMidCall struct {
	freshProvider
	replace func()
}

func (p *replacedMidCall) CreateIndex(ctx context.Context, in *protocol.CreateIndexRequest, opts ...grpc.CallOption) (*protocol.CreateIndexResponse, error) {
	p.replace()
	return p.freshProvider.CreateIndex(ctx, in, opts...)
}

// An index a call to the old provider made, ending after the new provider
// registered, is not taken as made for the new one.
func TestAnIndexMadeByAProviderReplacedMidCallIsMadeAgain(t *testing.T) {
	router := services.NewVectorSearchServer("token", zap.NewNop().Sugar())
	index := &routedIndex{router: func() (*services.VectorSearchServer, string, *protocol.Refusal) {
		return router, "token", nil
	}}
	second := &freshProvider{made: map[string]bool{}}
	first := &replacedMidCall{freshProvider: freshProvider{made: map[string]bool{}}, replace: func() {
		router.SetService(second)
		index.providerChanged()
	}}
	router.SetService(first)

	_ = index.Keep("m", 2, "a", []float32{1, 0})
	require.NoError(t, index.Keep("m", 2, "b", []float32{0, 1}), "the new provider is asked to make embeddings.m")
	assert.True(t, second.made["embeddings.m"])
}
