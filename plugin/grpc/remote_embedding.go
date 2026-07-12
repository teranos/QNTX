package grpc

import (
	"context"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	serverembeddings "github.com/teranos/QNTX/server/embeddings"
	"github.com/teranos/errors"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
	conn, err := grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to Embedding gRPC endpoint")
	}

	client := protocol.NewEmbeddingServiceClient(conn)

	return &RemoteEmbedding{
		client:    client,
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
// Empty model uses the plugin's default model.
func (r *RemoteEmbedding) Embed(text, model string) (*serverembeddings.EmbeddingResult, error) {
	resp, err := r.client.Embed(r.ctx, &protocol.EmbedRequest{
		AuthToken: r.authToken,
		Text:      text,
		Model:     model,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "gRPC Embed failed for text (%d chars)", len(text))
	}

	return &serverembeddings.EmbeddingResult{
		Text:      text,
		Embedding: resp.Vector,
		Tokens:    int(resp.Tokens),
	}, nil
}

// BatchEmbed generates vector embeddings for multiple texts.
// Empty model uses the plugin's default model.
func (r *RemoteEmbedding) BatchEmbed(texts []string, model string) (*serverembeddings.BatchEmbeddingResult, error) {
	resp, err := r.client.BatchEmbed(r.ctx, &protocol.BatchEmbedRequest{
		AuthToken: r.authToken,
		Texts:     texts,
		Model:     model,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "gRPC BatchEmbed failed for %d texts", len(texts))
	}

	results := make([]serverembeddings.EmbeddingResult, len(resp.Results))
	for i, vec := range resp.Results {
		text := ""
		if i < len(texts) {
			text = texts[i]
		}
		results[i] = serverembeddings.EmbeddingResult{
			Text:      text,
			Embedding: vec.Vector,
			Tokens:    int(vec.Tokens),
		}
	}

	return &serverembeddings.BatchEmbeddingResult{
		Embeddings:  results,
		TotalTokens: int(resp.TotalTokens),
	}, nil
}
