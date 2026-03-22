package provider

import (
	"context"

	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// GRPCLLMClient adapts the gRPC LLMService to the AIClient interface.
// This allows gRPC LLM plugins (like llama-cpp) to be used anywhere
// a local or cloud AIClient is expected.
type GRPCLLMClient struct {
	router   GRPCLLMRouter
	provider string
}

// GRPCLLMRouter is the interface for routing LLM requests to gRPC providers.
// Satisfied by grpc.LLMServer.
type GRPCLLMRouter interface {
	Chat(ctx context.Context, req *protocol.LLMChatRequest) (*protocol.LLMChatResponse, error)
}

// NewGRPCLLMClient creates an AIClient that routes through the gRPC LLM service.
func NewGRPCLLMClient(router GRPCLLMRouter, provider string) AIClient {
	return &GRPCLLMClient{router: router, provider: provider}
}

// Chat implements AIClient by forwarding to the gRPC LLM router.
func (c *GRPCLLMClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	grpcReq := &protocol.LLMChatRequest{
		SystemPrompt: req.SystemPrompt,
		UserPrompt:   req.UserPrompt,
		Provider:     c.provider,
	}

	if req.Temperature != nil {
		grpcReq.Temperature = *req.Temperature
	}
	if req.MaxTokens != nil {
		grpcReq.MaxTokens = int32(*req.MaxTokens)
	}
	if req.Model != nil {
		grpcReq.Model = *req.Model
	}

	for _, att := range req.Attachments {
		if att.ImageURL != nil {
			grpcReq.Attachments = append(grpcReq.Attachments, &protocol.Attachment{
				MimeType: "image",
				Data:     att.ImageURL.URL,
			})
		}
		if att.File != nil {
			grpcReq.Attachments = append(grpcReq.Attachments, &protocol.Attachment{
				MimeType: "file",
				Data:     att.File.FileData,
				Filename: att.File.Filename,
			})
		}
	}

	resp, err := c.router.Chat(ctx, grpcReq)
	if err != nil {
		return nil, errors.Wrapf(err, "gRPC LLM chat via provider %s failed", c.provider)
	}

	return &ChatResponse{
		Content: resp.Content,
		Model:   resp.Model,
		Usage: Usage{
			PromptTokens:     int(resp.PromptTokens),
			CompletionTokens: int(resp.CompletionTokens),
			TotalTokens:      int(resp.TotalTokens),
		},
	}, nil
}

var _ AIClient = (*GRPCLLMClient)(nil)
