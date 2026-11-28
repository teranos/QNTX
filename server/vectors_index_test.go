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
	// held is each index's ids, each with the revision it was added with.
	held map[string]map[string]int64
}

func (p *freshProvider) CreateIndex(_ context.Context, in *protocol.CreateIndexRequest, _ ...grpc.CallOption) (*protocol.CreateIndexResponse, error) {
	p.made[in.GetName()] = true
	return &protocol.CreateIndexResponse{Name: in.GetName()}, nil
}

func (p *freshProvider) AddVectors(_ context.Context, in *protocol.AddVectorsRequest, _ ...grpc.CallOption) (*protocol.AddVectorsResponse, error) {
	if !p.made[in.GetIndex()] {
		return nil, assert.AnError
	}
	if p.held == nil {
		p.held = map[string]map[string]int64{}
	}
	if p.held[in.GetIndex()] == nil {
		p.held[in.GetIndex()] = map[string]int64{}
	}
	for _, v := range in.GetVectors() {
		p.held[in.GetIndex()][v.GetId()] = v.GetRevision()
	}
	return &protocol.AddVectorsResponse{Added: int32(len(in.GetVectors()))}, nil
}

func (p *freshProvider) Holds(_ context.Context, in *protocol.HoldsRequest, _ ...grpc.CallOption) (*protocol.HoldsResponse, error) {
	answer := &protocol.HoldsResponse{}
	for _, id := range in.GetIds() {
		if revision, held := p.held[in.GetIndex()][id]; held {
			answer.Held = append(answer.Held, &protocol.HeldVector{Id: id, Revision: revision})
		}
	}
	return answer, nil
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

// What an index holds is answered through the router by the ids asked about,
// each with the revision it was added with; an id never added, and an index
// never made, hold nothing.
func TestAnIndexSaysWhichIdsItHoldsAndAtWhatRevision(t *testing.T) {
	router := services.NewVectorSearchServer("token", zap.NewNop().Sugar())
	router.SetService(&freshProvider{made: map[string]bool{"said": true}})
	ctx := context.Background()

	_, err := router.AddVectors(ctx, &protocol.AddVectorsRequest{AuthToken: "token", Index: "said", Vectors: []*protocol.VectorEntry{
		{Id: "prompt", Vector: []float32{1, 0}, Revision: 1},
		{Id: "message", Vector: []float32{0, 1}, Revision: 7},
	}})
	require.NoError(t, err)

	held, err := router.Holds(ctx, &protocol.HoldsRequest{AuthToken: "token", Index: "said", Ids: []string{"prompt", "message", "never"}})
	require.NoError(t, err)
	assert.Equal(t, []*protocol.HeldVector{{Id: "prompt", Revision: 1}, {Id: "message", Revision: 7}}, held.GetHeld())

	none, err := router.Holds(ctx, &protocol.HoldsRequest{AuthToken: "token", Index: "unmade", Ids: []string{"prompt"}})
	require.NoError(t, err)
	assert.Empty(t, none.GetHeld())

	_, err = router.Holds(ctx, &protocol.HoldsRequest{AuthToken: "wrong", Index: "said", Ids: []string{"prompt"}})
	assert.Error(t, err, "a call with another token is refused")
}
