package grpc

import (
	"context"

	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	errors "github.com/teranos/sacred-error"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// RemoteLLM is a gRPC client wrapper for the LLM service.
// It implements plugin.LLMService for remote plugins.
type RemoteLLM struct {
	client protocol.LLMServiceClient
	conn   *grpc.ClientConn
	logger *zap.SugaredLogger
}

// NewRemoteLLM creates a gRPC client connection to the core LLM service.
func NewRemoteLLM(ctx context.Context, endpoint string, logger *zap.SugaredLogger) (*RemoteLLM, error) {
	conn, err := grpc.NewClient(endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to connect to LLM service at %s", endpoint)
	}

	client := protocol.NewLLMServiceClient(conn)

	return &RemoteLLM{
		client: client,
		conn:   conn,
		logger: logger,
	}, nil
}

// Close closes the gRPC connection.
func (r *RemoteLLM) Close() error {
	return r.conn.Close()
}

// Chat sends an LLM chat request via gRPC.
func (r *RemoteLLM) Chat(ctx context.Context, req plugin.LLMRequest) (*plugin.LLMResponse, error) {
	attachments := make([]*protocol.Attachment, len(req.Attachments))
	for i, a := range req.Attachments {
		attachments[i] = &protocol.Attachment{
			MimeType: a.MimeType,
			Data:     a.Data,
			Filename: a.Filename,
		}
	}

	resp, err := r.client.Chat(ctx, &protocol.LLMChatRequest{
		SystemPrompt: req.SystemPrompt,
		UserPrompt:   req.UserPrompt,
		Model:        req.Model,
		Temperature:  req.Temperature,
		MaxTokens:    int32(req.MaxTokens),
		Provider:     req.Provider,
		Attachments:  attachments,
	})
	if err != nil {
		return nil, errors.Wrap(err, "LLM service gRPC call failed")
	}

	return &plugin.LLMResponse{
		Content:          resp.Content,
		Model:            resp.Model,
		PromptTokens:     int(resp.PromptTokens),
		CompletionTokens: int(resp.CompletionTokens),
		TotalTokens:      int(resp.TotalTokens),
		Served:           servedFromProto(resp.GetServed()),
	}, nil
}

// servedFromProto is a provider's report of how a call was served, or nil for
// a response that carries none.
func servedFromProto(served *protocol.LLMServed) *plugin.LLMServed {
	if served == nil {
		return nil
	}
	return &plugin.LLMServed{
		RequestedProvider: served.GetRequestedProvider(),
		RequestedModel:    served.GetRequestedModel(),
		UpstreamProvider:  served.GetUpstreamProvider(),
		ExecutedModel:     served.GetExecutedModel(),
		ResponseModel:     served.GetResponseModel(),
		RequestID:         served.GetRequestId(),
		CostUSD:           served.CostUsd,
		PricingSource:     served.GetPricingSource(),
		AttemptCount:      int(served.GetAttemptCount()),
		Fallback:          served.GetFallback(),
	}
}

// servedToProto is the wire form of a provider's report, or nil for none.
func servedToProto(served *plugin.LLMServed) *protocol.LLMServed {
	if served == nil {
		return nil
	}
	return &protocol.LLMServed{
		RequestedProvider: served.RequestedProvider,
		RequestedModel:    served.RequestedModel,
		UpstreamProvider:  served.UpstreamProvider,
		ExecutedModel:     served.ExecutedModel,
		ResponseModel:     served.ResponseModel,
		RequestId:         served.RequestID,
		CostUsd:           served.CostUSD,
		PricingSource:     served.PricingSource,
		AttemptCount:      int32(served.AttemptCount),
		Fallback:          served.Fallback,
	}
}
