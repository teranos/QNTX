package grpc

import (
	"context"

	"github.com/teranos/QNTX/ats/embeddings/embeddings"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// RemoteEmbedding is a gRPC client wrapper for the EmbeddingService.
// Plugins use this to request embeddings from the core server.
type RemoteEmbedding struct {
	client    protocol.EmbeddingServiceClient
	conn      *grpc.ClientConn
	authToken string
	logger    *zap.SugaredLogger
	ctx       context.Context
}

// NewRemoteEmbedding creates a gRPC client connection to the EmbeddingService.
func NewRemoteEmbedding(ctx context.Context, endpoint string, authToken string, logger *zap.SugaredLogger) (*RemoteEmbedding, error) {
	conn, err := dialPluginEndpoint(endpoint, "EmbeddingService")
	if err != nil {
		return nil, err
	}

	return &RemoteEmbedding{
		client:    protocol.NewEmbeddingServiceClient(conn),
		conn:      conn,
		authToken: authToken,
		logger:    logger,
		ctx:       ctx,
	}, nil
}

// Close closes the gRPC connection.
func (r *RemoteEmbedding) Close() error {
	return r.conn.Close()
}

// Embed generates a vector embedding for a single text.
func (r *RemoteEmbedding) Embed(text string) (*embeddings.EmbeddingResult, error) {
	resp, err := r.client.Embed(r.ctx, &protocol.EmbedRequest{
		AuthToken: r.authToken,
		Text:      text,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "gRPC Embed failed for text (%d chars)", len(text))
	}

	return &embeddings.EmbeddingResult{
		Text:      text,
		Embedding: resp.Vector,
		Tokens:    int(resp.Tokens),
	}, nil
}

// BatchEmbed generates vector embeddings for multiple texts.
func (r *RemoteEmbedding) BatchEmbed(texts []string) (*embeddings.BatchEmbeddingResult, error) {
	resp, err := r.client.BatchEmbed(r.ctx, &protocol.BatchEmbedRequest{
		AuthToken: r.authToken,
		Texts:     texts,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "gRPC BatchEmbed failed for %d texts", len(texts))
	}

	results := make([]embeddings.EmbeddingResult, len(resp.Results))
	for i, vec := range resp.Results {
		text := ""
		if i < len(texts) {
			text = texts[i]
		}
		results[i] = embeddings.EmbeddingResult{
			Text:      text,
			Embedding: vec.Vector,
			Tokens:    int(vec.Tokens),
		}
	}

	return &embeddings.BatchEmbeddingResult{
		Embeddings:  results,
		TotalTokens: int(resp.TotalTokens),
	}, nil
}
