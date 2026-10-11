package grpc

import (
	"context"
	"fmt"

	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/errors"
	"go.uber.org/zap"
)

// A service the node runs without reaches a plugin as a value saying so: each
// call to it is refused with why, rather than dialled at an address that is
// not there.

// llmAt is the LLM service at e: a client of it, or one refusing with why the
// node runs without it.
func llmAt(ctx context.Context, e ServiceEndpoint, logger *zap.SugaredLogger) (plugin.LLMService, error) {
	switch e.how {
	case servingAt:
		return NewRemoteLLM(ctx, e.at, logger)
	case notRunning:
		return notRunningLLM{why: e.why}, nil
	}
	return nil, errors.Newf("the llm endpoint names nothing (kind %d)", e.how)
}

// vectorSearchAt is the vector search service at e, or one refusing with why
// the node runs without it.
func vectorSearchAt(ctx context.Context, e ServiceEndpoint, authToken string, logger *zap.SugaredLogger) (plugin.VectorSearchService, error) {
	switch e.how {
	case servingAt:
		return NewRemoteVectorSearch(ctx, e.at, authToken, logger)
	case notRunning:
		return notRunningVectorSearch{why: e.why}, nil
	}
	return nil, errors.Newf("the vector search endpoint names nothing (kind %d)", e.how)
}

// searchAt is the search service at e, or one refusing with why the node runs
// without it.
func searchAt(ctx context.Context, e ServiceEndpoint, logger *zap.SugaredLogger) (plugin.SearchService, error) {
	switch e.how {
	case servingAt:
		return NewRemoteSearch(ctx, e.at, logger)
	case notRunning:
		return notRunningSearch{why: e.why}, nil
	}
	return nil, errors.Newf("the search endpoint names nothing (kind %d)", e.how)
}

// notRunningLLM is the LLM service of a node running without it.
type notRunningLLM struct{ why error }

func (n notRunningLLM) Chat(context.Context, plugin.LLMRequest) (*plugin.LLMResponse, error) {
	return nil, n.why
}

// notRunningVectorSearch is the vector search service of a node running
// without it.
type notRunningVectorSearch struct{ why error }

func (n notRunningVectorSearch) Search(context.Context, plugin.VectorSearchRequest) (*plugin.VectorSearchResponse, error) {
	return nil, n.why
}

func (n notRunningVectorSearch) AddVectors(context.Context, plugin.AddVectorsRequest) (*plugin.AddVectorsResponse, error) {
	return nil, n.why
}

func (n notRunningVectorSearch) CreateIndex(context.Context, plugin.CreateIndexRequest) (*plugin.CreateIndexResponse, error) {
	return nil, n.why
}

// notRunningSearch is the search service of a node running without it.
type notRunningSearch struct{ why error }

func (n notRunningSearch) Search(context.Context, plugin.SearchRequest) (*plugin.SearchResponse, error) {
	return nil, n.why
}

func (n notRunningSearch) IndexDocuments(context.Context, plugin.IndexDocumentsRequest) (*plugin.IndexDocumentsResponse, error) {
	return nil, n.why
}

func (n notRunningSearch) DeleteDocuments(context.Context, plugin.DeleteDocumentsRequest) (*plugin.DeleteDocumentsResponse, error) {
	return nil, n.why
}

func (n notRunningSearch) ConfigureIndex(context.Context, plugin.ConfigureIndexRequest) (*plugin.ConfigureIndexResponse, error) {
	return nil, n.why
}

// configKey is how a Go plugin finds the endpoint in its config: the address
// under _<service>_endpoint, or why the node runs without it under
// _<service>_not_running.
func (e ServiceEndpoint) configKey(service string) (key, value string) {
	switch e.how {
	case servingAt:
		return "_" + service + "_endpoint", e.at
	case notRunning:
		return "_" + service + "_not_running", e.why.Error()
	}
	return "_" + service + "_not_running", fmt.Sprintf("the %s endpoint names nothing (kind %d)", service, e.how)
}
