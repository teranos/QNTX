package server

import (
	"context"
	"sync"
	"time"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	errors "github.com/teranos/sacred-error"
)

// routedIndex is where the embedding store keeps and finds its vectors:
// vector search, a service a plugin provides (ADR-016), through the router the
// provider registered with. One index per model (ADR-019), made the first time
// a vector of that model is kept.
type routedIndex struct {
	s    *QNTXServer
	made sync.Map // index names made with the provider registered now
	// router is the vector search router and its token; the node's own when
	// nil.
	router func() (*services.VectorSearchServer, string, *protocol.Refusal)
}

// vectorRouter is the router this index reaches vector search through.
func (r *routedIndex) vectorRouter() (*services.VectorSearchServer, string, *protocol.Refusal) {
	if r.router != nil {
		return r.router()
	}
	return r.s.vectorRouter()
}

// providerChanged forgets which indexes were made: a provider that registers
// may hold none of them, and is asked to make each again.
func (r *routedIndex) providerChanged() {
	r.made.Clear()
}

// indexCallTimeout bounds one call to the vector search provider.
const indexCallTimeout = 30 * time.Second

// embeddingsIndex is the index a model's embeddings are kept in.
func embeddingsIndex(model string) string { return "embeddings." + model }

func (r *routedIndex) Keep(model string, dimensions int, id string, vector []float32) error {
	router, token, refused := r.vectorRouter()
	if refused != nil {
		return errors.New(refused.GetSays())
	}
	ctx, cancel := context.WithTimeout(context.Background(), indexCallTimeout)
	defer cancel()
	index := embeddingsIndex(model)
	if _, made := r.made.Load(index); !made {
		if _, err := router.CreateIndex(ctx, &protocol.CreateIndexRequest{AuthToken: token, Name: index, Dimensions: int32(dimensions)}); err != nil {
			return errors.Wrapf(err, "the index %s of %d dimensions was not made", index, dimensions)
		}
		r.made.Store(index, true)
	}
	if _, err := router.AddVectors(ctx, &protocol.AddVectorsRequest{AuthToken: token, Index: index,
		Vectors: []*protocol.VectorEntry{{Id: id, Vector: vector}}}); err != nil {
		return errors.Wrapf(err, "%s was not added to %s", id, index)
	}
	return nil
}

func (r *routedIndex) Forget(model string, ids []string) error {
	router, token, refused := r.vectorRouter()
	if refused != nil {
		return errors.New(refused.GetSays())
	}
	ctx, cancel := context.WithTimeout(context.Background(), indexCallTimeout)
	defer cancel()
	if _, err := router.DeleteVectors(ctx, &protocol.DeleteVectorsRequest{AuthToken: token, Index: embeddingsIndex(model), Ids: ids}); err != nil {
		return errors.Wrapf(err, "%d vectors were not taken out of %s", len(ids), embeddingsIndex(model))
	}
	return nil
}

func (r *routedIndex) Nearest(model string, query []float32, limit int) ([]storage.VectorHit, error) {
	router, token, refused := r.vectorRouter()
	if refused != nil {
		return nil, errors.New(refused.GetSays())
	}
	ctx, cancel := context.WithTimeout(context.Background(), indexCallTimeout)
	defer cancel()
	if model == "" {
		embedding, token, refused := r.s.embeddingRouter()
		if refused != nil {
			return nil, errors.New(refused.GetSays())
		}
		info, err := embedding.ModelInfo(ctx, &protocol.ModelInfoRequest{AuthToken: token})
		if err != nil {
			return nil, errors.Wrap(err, "which model the embedding provider embeds with first was not answered")
		}
		model = info.GetName()
	}
	answered, err := router.Search(ctx, &protocol.VectorSearchRequest{AuthToken: token, Index: embeddingsIndex(model), QueryVector: query, TopK: int32(limit)})
	if err != nil {
		return nil, errors.Wrapf(err, "%s was not searched", embeddingsIndex(model))
	}
	hits := make([]storage.VectorHit, 0, len(answered.GetResults()))
	for _, hit := range answered.GetResults() {
		hits = append(hits, storage.VectorHit{ID: hit.GetId(), Distance: hit.GetDistance()})
	}
	return hits, nil
}

// VectorSearchProvided is a vector search provider registering: what the
// embedding store kept while none was, or before it had an index, is offered
// to it, off the caller's time.
func (s *QNTXServer) VectorSearchProvided() {
	if s.vectorIndex != nil {
		s.vectorIndex.providerChanged()
	}
	if s.embeddingStore == nil {
		return
	}
	store := s.embeddingStore
	go func() {
		kept, err := store.IndexKept()
		if err != nil {
			s.logger.Errorw("Kept embeddings were not all indexed by the vector search provider", "indexed", kept, "error", err)
			return
		}
		s.logger.Infow("Kept embeddings indexed by the vector search provider", "indexed", kept)
	}()
}
