package grpc

import (
	"context"

	"github.com/teranos/errors"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// RemoteVectorSearch is a gRPC client wrapper for the VectorSearchService.
// Plugins use this to search vector indexes via the core server.
type RemoteVectorSearch struct {
	client    protocol.VectorSearchServiceClient
	conn      *grpc.ClientConn
	authToken string
	logger    *zap.SugaredLogger
	ctx       context.Context
}

// NewRemoteVectorSearch creates a gRPC client connection to the VectorSearchService.
func NewRemoteVectorSearch(ctx context.Context, endpoint string, authToken string, logger *zap.SugaredLogger) (*RemoteVectorSearch, error) {
	conn, err := grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, errors.Wrap(err, "failed to connect to VectorSearch gRPC endpoint")
	}

	client := protocol.NewVectorSearchServiceClient(conn)

	return &RemoteVectorSearch{
		client:    client,
		conn:      conn,
		authToken: authToken,
		logger:    logger,
		ctx:       ctx,
	}, nil
}

// Close closes the gRPC connection.
func (r *RemoteVectorSearch) Close() error {
	return r.conn.Close()
}

// Search finds the nearest neighbors to a query vector.
func (r *RemoteVectorSearch) Search(ctx context.Context, req plugin.VectorSearchRequest) (*plugin.VectorSearchResponse, error) {
	resp, err := r.client.Search(ctx, &protocol.VectorSearchRequest{
		AuthToken:   r.authToken,
		Index:       req.Index,
		QueryVector: req.QueryVector,
		TopK:        int32(req.TopK),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "gRPC VectorSearch failed for index %s (top_k=%d)", req.Index, req.TopK)
	}

	hits := make([]plugin.VectorSearchHit, len(resp.Results))
	for i, h := range resp.Results {
		hits[i] = plugin.VectorSearchHit{
			ID:       h.Id,
			Distance: h.Distance,
		}
	}

	return &plugin.VectorSearchResponse{Results: hits}, nil
}

// AddVectors inserts vectors into a named index.
func (r *RemoteVectorSearch) AddVectors(ctx context.Context, req plugin.AddVectorsRequest) (*plugin.AddVectorsResponse, error) {
	entries := make([]*protocol.VectorEntry, len(req.Vectors))
	for i, v := range req.Vectors {
		entries[i] = &protocol.VectorEntry{
			Id:     v.ID,
			Vector: v.Vector,
		}
	}

	resp, err := r.client.AddVectors(ctx, &protocol.AddVectorsRequest{
		AuthToken: r.authToken,
		Index:     req.Index,
		Vectors:   entries,
	})
	if err != nil {
		return nil, errors.Wrapf(err, "gRPC AddVectors failed for index %s (%d vectors)", req.Index, len(req.Vectors))
	}

	return &plugin.AddVectorsResponse{Added: int(resp.Added)}, nil
}

// CreateIndex creates a new named vector index.
func (r *RemoteVectorSearch) CreateIndex(ctx context.Context, req plugin.CreateIndexRequest) (*plugin.CreateIndexResponse, error) {
	resp, err := r.client.CreateIndex(ctx, &protocol.CreateIndexRequest{
		AuthToken:  r.authToken,
		Name:       req.Name,
		Dimensions: int32(req.Dimensions),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "gRPC CreateIndex failed for index %s (dims=%d)", req.Name, req.Dimensions)
	}

	return &plugin.CreateIndexResponse{Name: resp.Name}, nil
}
