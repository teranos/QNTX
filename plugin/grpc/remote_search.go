package grpc

import (
	"context"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// RemoteSearch is a gRPC client wrapper for the SearchService.
// It implements plugin.SearchService for remote plugins.
type RemoteSearch struct {
	client protocol.SearchServiceClient
	conn   *grpc.ClientConn
	logger *zap.SugaredLogger
}

// NewRemoteSearch creates a gRPC client connection to the core SearchService.
func NewRemoteSearch(ctx context.Context, endpoint string, logger *zap.SugaredLogger) (*RemoteSearch, error) {
	conn, err := grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to connect to search service at %s", endpoint)
	}

	client := protocol.NewSearchServiceClient(conn)

	return &RemoteSearch{
		client: client,
		conn:   conn,
		logger: logger,
	}, nil
}

// Close closes the gRPC connection.
func (r *RemoteSearch) Close() error {
	return r.conn.Close()
}

// Search sends a search request via gRPC.
func (r *RemoteSearch) Search(ctx context.Context, req plugin.SearchRequest) (*plugin.SearchResponse, error) {
	resp, err := r.client.Search(ctx, &protocol.SearchRequest{
		Query:   req.Query,
		Index:   req.Index,
		TopK:    int32(req.TopK),
		Filters: req.Filters,
	})
	if err != nil {
		return nil, errors.Wrap(err, "search service gRPC call failed")
	}

	hits := make([]plugin.SearchHit, len(resp.Hits))
	for i, h := range resp.Hits {
		hits[i] = plugin.SearchHit{
			ID:       h.Id,
			Score:    h.Score,
			Document: h.Document,
		}
	}

	return &plugin.SearchResponse{
		Hits:         hits,
		Total:        int(resp.Total),
		ProcessingMs: int(resp.ProcessingMs),
	}, nil
}

// IndexDocuments sends an index request via gRPC.
func (r *RemoteSearch) IndexDocuments(ctx context.Context, req plugin.IndexDocumentsRequest) (*plugin.IndexDocumentsResponse, error) {
	resp, err := r.client.IndexDocuments(ctx, &protocol.IndexDocumentsRequest{
		Index:     req.Index,
		Documents: req.Documents,
	})
	if err != nil {
		return nil, errors.Wrap(err, "index documents gRPC call failed")
	}

	return &plugin.IndexDocumentsResponse{
		Accepted: int(resp.Accepted),
	}, nil
}

// DeleteDocuments sends a delete request via gRPC.
func (r *RemoteSearch) DeleteDocuments(ctx context.Context, req plugin.DeleteDocumentsRequest) (*plugin.DeleteDocumentsResponse, error) {
	resp, err := r.client.DeleteDocuments(ctx, &protocol.DeleteDocumentsRequest{
		Index: req.Index,
		Ids:   req.IDs,
	})
	if err != nil {
		return nil, errors.Wrap(err, "delete documents gRPC call failed")
	}

	return &plugin.DeleteDocumentsResponse{
		Deleted: int(resp.Deleted),
	}, nil
}
