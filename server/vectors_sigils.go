package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/grpc/services"
	"github.com/teranos/QNTX/server/reach"
	"github.com/teranos/QNTX/server/sigil"
)

// The services plugins provide, as signa: vector search (ADR-016) and
// embedding (ADR-017), each answered through the router the provider plugin
// registered with. Asked over HTTP and by subsystems; not everything needs to
// be a sigil an LLM calls.

const vectorsPath = "/api/vectors/{index}"

// providedSurfaces is where the provided services are asked.
var providedSurfaces = []string{reach.OverHTTP, reach.OverSubsystem}

func (s *QNTXServer) vectorsSignum() sigil.Signum {
	index := &protocol.Param{Name: "index", Required: true, Says: "The index, one per model: its vectors all have the model's dimensions."}
	return sigil.Signum{
		Signum: &protocol.Signum{
			Name:        "vectors",
			Description: "Vector search, a service a plugin provides (ADR-016): indexes of vectors, each found by its nearest.",
			Tags:        []string{"vectors", "search", "embeddings"},
			Sigils: []*protocol.Sigil{
				{
					Name: "create index",
					Does: "Make an index for vectors of a number of dimensions. An index that is there already with those dimensions is no error.",
					Takes: []*protocol.Param{index,
						{Name: "dimensions", Required: true, Kind: sigil.Count, Says: "How many numbers each vector in it holds."}},
					Answer:   "protocol.CreateIndexResponse",
					Http:     &protocol.Endpoint{Method: http.MethodPut, Path: vectorsPath},
					Surfaces: providedSurfaces,
				},
				{
					Name: "add",
					Does: "Put vectors into an index, each under its id. An id added again is replaced, not doubled.",
					Takes: []*protocol.Param{index,
						{Name: "vectors", Required: true, Kind: sigil.List, Says: "The vectors, each an object with its id and its vector, a list of numbers."}},
					Answer:   "protocol.AddVectorsResponse",
					Http:     &protocol.Endpoint{Method: http.MethodPost, Path: vectorsPath + "/add"},
					Surfaces: providedSurfaces,
				},
				{
					Name: "delete",
					Does: "Take vectors out of an index by their ids. An id the index does not hold is no error.",
					Takes: []*protocol.Param{index,
						{Name: "ids", Required: true, Kind: sigil.List, Says: "The ids of the vectors to take out."}},
					Answer:   "protocol.DeleteVectorsResponse",
					Http:     &protocol.Endpoint{Method: http.MethodPost, Path: vectorsPath + "/delete"},
					Surfaces: providedSurfaces,
				},
				{
					Name: "search",
					Does: "The vectors of an index nearest to one vector, nearest first, each by its id and its distance.",
					Takes: []*protocol.Param{index,
						{Name: "vector", Required: true, Kind: sigil.List, Says: "The vector to search from, a list of numbers."},
						{Name: "top_k", Required: true, Kind: sigil.Count, Says: "How many of the nearest to answer."}},
					Answer:   "protocol.VectorSearchResponse",
					Http:     &protocol.Endpoint{Method: http.MethodPost, Path: vectorsPath + "/search"},
					Surfaces: providedSurfaces,
				},
			},
		},
		Answers: map[string]sigil.Answer{
			"create index": s.vectorsCreateIndex,
			"add":          s.vectorsAdd,
			"delete":       s.vectorsDelete,
			"search":       s.vectorsSearch,
		},
	}
}

// failedProvided is a provided service that did not answer, in its words.
func failedProvided(service string, err error) *protocol.Refusal {
	return &protocol.Refusal{Why: sigil.Failed, Says: service + ": " + err.Error()}
}

// vectorRouter is the vector search router and the token it was handed, or
// why there is none.
func (s *QNTXServer) vectorRouter() (*services.VectorSearchServer, string, *protocol.Refusal) {
	if s.servicesManager == nil {
		return nil, "", &protocol.Refusal{Why: sigil.Failed, Says: "the node runs no services for plugins, so no vector search"}
	}
	router := s.servicesManager.GetVectorSearchRouter()
	if router == nil {
		return nil, "", &protocol.Refusal{Why: sigil.Failed, Says: "the node runs no vector search service"}
	}
	return router, s.servicesManager.Endpoints().AuthToken, nil
}

// embeddingRouter is the embedding router and the token it was handed, or why
// there is none.
func (s *QNTXServer) embeddingRouter() (*services.EmbeddingServer, string, *protocol.Refusal) {
	if s.servicesManager == nil {
		return nil, "", &protocol.Refusal{Why: sigil.Failed, Says: "the node runs no services for plugins, so no embedding"}
	}
	router := s.servicesManager.GetEmbeddingRouter()
	if router == nil {
		return nil, "", &protocol.Refusal{Why: sigil.Failed, Says: "the node runs no embedding service"}
	}
	return router, s.servicesManager.Endpoints().AuthToken, nil
}

// listOf is a list param read into into, or the refusal naming it.
func listOf(sent sigil.Sent, param string, into any) *protocol.Refusal {
	if err := json.Unmarshal([]byte(sent[param]), into); err != nil {
		return &protocol.Refusal{Why: sigil.Invalid, Param: param, Says: param + " does not read: " + err.Error()}
	}
	return nil
}

// sentCount is a count param, read: sigil.Read has refused one that is not.
func sentCount(sent sigil.Sent, param string) int32 {
	n, err := strconv.Atoi(sent[param])
	if err != nil {
		return 0
	}
	return int32(n)
}

func (s *QNTXServer) vectorsCreateIndex(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	router, token, refused := s.vectorRouter()
	if refused != nil {
		return nil, refused
	}
	answered, err := router.CreateIndex(ctx, &protocol.CreateIndexRequest{AuthToken: token, Name: sent["index"], Dimensions: sentCount(sent, "dimensions")})
	if err != nil {
		return nil, failedProvided("vector search", err)
	}
	return answered, nil
}

// vectorEntry is one vector as the add sigil takes it.
type vectorEntry struct {
	ID     string    `json:"id"`
	Vector []float32 `json:"vector"`
}

func (s *QNTXServer) vectorsAdd(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	var entries []vectorEntry
	if refused := listOf(sent, "vectors", &entries); refused != nil {
		return nil, refused
	}
	vectors := make([]*protocol.VectorEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.ID == "" {
			return nil, &protocol.Refusal{Why: sigil.Invalid, Param: "vectors", Says: "a vector was sent with no id"}
		}
		vectors = append(vectors, &protocol.VectorEntry{Id: entry.ID, Vector: entry.Vector})
	}
	router, token, refused := s.vectorRouter()
	if refused != nil {
		return nil, refused
	}
	answered, err := router.AddVectors(ctx, &protocol.AddVectorsRequest{AuthToken: token, Index: sent["index"], Vectors: vectors})
	if err != nil {
		return nil, failedProvided("vector search", err)
	}
	return answered, nil
}

func (s *QNTXServer) vectorsDelete(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	var ids []string
	if refused := listOf(sent, "ids", &ids); refused != nil {
		return nil, refused
	}
	router, token, refused := s.vectorRouter()
	if refused != nil {
		return nil, refused
	}
	answered, err := router.DeleteVectors(ctx, &protocol.DeleteVectorsRequest{AuthToken: token, Index: sent["index"], Ids: ids})
	if err != nil {
		return nil, failedProvided("vector search", err)
	}
	return answered, nil
}

func (s *QNTXServer) vectorsSearch(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	var vector []float32
	if refused := listOf(sent, "vector", &vector); refused != nil {
		return nil, refused
	}
	router, token, refused := s.vectorRouter()
	if refused != nil {
		return nil, refused
	}
	answered, err := router.Search(ctx, &protocol.VectorSearchRequest{AuthToken: token, Index: sent["index"], QueryVector: vector, TopK: sentCount(sent, "top_k")})
	if err != nil {
		return nil, failedProvided("vector search", err)
	}
	return answered, nil
}

func (s *QNTXServer) embeddingsGenerate(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	router, token, refused := s.embeddingRouter()
	if refused != nil {
		return nil, refused
	}
	answered, err := router.Embed(ctx, &protocol.EmbedRequest{AuthToken: token, Text: sent["text"], Model: sent["model"]})
	if err != nil {
		return nil, failedProvided("embedding", err)
	}
	return answered, nil
}

func (s *QNTXServer) embeddingsBatch(ctx context.Context, sent sigil.Sent) (any, *protocol.Refusal) {
	var texts []string
	if refused := listOf(sent, "texts", &texts); refused != nil {
		return nil, refused
	}
	router, token, refused := s.embeddingRouter()
	if refused != nil {
		return nil, refused
	}
	answered, err := router.BatchEmbed(ctx, &protocol.BatchEmbedRequest{AuthToken: token, Texts: texts, Model: sent["model"]})
	if err != nil {
		return nil, failedProvided("embedding", err)
	}
	return answered, nil
}
