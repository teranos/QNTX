package qntxatproto

import (
	"context"
	"fmt"
	"net/http"

	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	timelineIndexName = "atproto-timeline"
	embeddingDims     = 384 // MiniLM-L6-v2
)

// embeddingClient wraps the EmbeddingService gRPC client.
type embeddingClient struct {
	client    protocol.EmbeddingServiceClient
	conn      *grpc.ClientConn
	authToken string
}

func newEmbeddingClient(endpoint, authToken string) (*embeddingClient, error) {
	conn, err := grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to connect to embedding endpoint %s", endpoint)
	}
	return &embeddingClient{
		client:    protocol.NewEmbeddingServiceClient(conn),
		conn:      conn,
		authToken: authToken,
	}, nil
}

func (c *embeddingClient) embed(ctx context.Context, text string) ([]float32, error) {
	resp, err := c.client.Embed(ctx, &protocol.EmbedRequest{
		AuthToken: c.authToken,
		Text:      text,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "embed failed for text (%d chars)", len(text))
	}
	return resp.Vector, nil
}

func (c *embeddingClient) close() {
	if c.conn != nil {
		c.conn.Close()
	}
}

// initVectorSearch sets up the embedding client and creates the timeline index.
// Called during Initialize if both services are available.
func (p *Plugin) initVectorSearch(ctx context.Context) error {
	logger := p.Services().Logger("atproto")
	config := p.Services().Config("atproto")

	embeddingEndpoint := config.GetString("_embedding_endpoint")
	if embeddingEndpoint == "" {
		logger.Debug("No embedding endpoint, vector search disabled")
		return nil
	}

	vs := p.Services().VectorSearch()
	if vs == nil {
		logger.Debug("No vector search provider, vector search disabled")
		return nil
	}

	authToken := config.GetString("_auth_token")
	ec, err := newEmbeddingClient(embeddingEndpoint, authToken)
	if err != nil {
		return errors.Wrap(err, "failed to create embedding client")
	}

	p.mu.Lock()
	p.embedding = ec
	p.mu.Unlock()

	// Create index (idempotent)
	if _, err := vs.CreateIndex(ctx, plugin.CreateIndexRequest{
		Name:       timelineIndexName,
		Dimensions: embeddingDims,
	}); err != nil {
		return errors.Wrapf(err, "failed to create index %s", timelineIndexName)
	}

	logger.Infow("Vector search initialized", "index", timelineIndexName, "dims", embeddingDims)
	return nil
}

// indexTimelinePost embeds a post and adds it to the vector index.
func (p *Plugin) indexTimelinePost(ctx context.Context, uri, text string) error {
	p.mu.RLock()
	ec := p.embedding
	p.mu.RUnlock()

	if ec == nil || text == "" {
		return nil
	}

	vs := p.Services().VectorSearch()
	if vs == nil {
		return nil
	}

	vec, err := ec.embed(ctx, text)
	if err != nil {
		return errors.Wrapf(err, "failed to embed post %s", uri)
	}

	if _, err := vs.AddVectors(ctx, plugin.AddVectorsRequest{
		Index: timelineIndexName,
		Vectors: []plugin.VectorEntry{
			{ID: uri, Vector: vec},
		},
	}); err != nil {
		return errors.Wrapf(err, "failed to index post %s", uri)
	}

	return nil
}

// handleSearchTimeline searches the timeline vector index by query text.
func (p *Plugin) handleSearchTimeline(w http.ResponseWriter, r *http.Request) {
	if p.checkPaused(w) {
		return
	}

	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "Query parameter 'q' is required")
		return
	}

	p.mu.RLock()
	ec := p.embedding
	p.mu.RUnlock()

	if ec == nil {
		writeError(w, http.StatusServiceUnavailable, "Vector search not available (no embedding endpoint)")
		return
	}

	vs := p.Services().VectorSearch()
	if vs == nil {
		writeError(w, http.StatusServiceUnavailable, "Vector search provider not available")
		return
	}

	// Embed query
	vec, err := ec.embed(r.Context(), query)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Failed to embed query: %v", err))
		return
	}

	topK := 10
	resp, err := vs.Search(r.Context(), plugin.VectorSearchRequest{
		Index:       timelineIndexName,
		QueryVector: vec,
		TopK:        topK,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("Search failed: %v", err))
		return
	}

	type searchResult struct {
		URI      string  `json:"uri"`
		Distance float32 `json:"distance"`
	}

	results := make([]searchResult, len(resp.Results))
	for i, hit := range resp.Results {
		results[i] = searchResult{URI: hit.ID, Distance: hit.Distance}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"query":   query,
		"results": results,
	})
}
