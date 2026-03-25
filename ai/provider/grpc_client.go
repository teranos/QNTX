package provider

import (
	"context"
	"io"

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
	StreamChatClient(ctx context.Context, req *protocol.LLMChatRequest) (protocol.LLMService_StreamChatClient, error)
}

// NewGRPCLLMClient creates an AIClient that routes through the gRPC LLM service.
// The returned client also implements StreamingAIClient — use type assertion to access streaming.
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

// ChatStreaming implements StreamingAIClient by forwarding to the gRPC StreamChat RPC.
func (c *GRPCLLMClient) ChatStreaming(ctx context.Context, req ChatRequest, streamChan chan<- StreamChunk) error {
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

	stream, err := c.router.StreamChatClient(ctx, grpcReq)
	if err != nil {
		return errors.Wrapf(err, "gRPC LLM stream chat via provider %s failed", c.provider)
	}

	defer close(streamChan)

	for {
		chunk, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return errors.Wrapf(err, "gRPC LLM stream recv from provider %s failed", c.provider)
		}

		sc := StreamChunk{
			Content: chunk.Token,
			Done:    chunk.Done,
			Model:   chunk.Model,
		}

		if chunk.Done {
			sc.PromptTokens = int(chunk.PromptTokens)
			sc.CompletionTokens = int(chunk.CompletionTokens)
			sc.TotalTokens = int(chunk.TotalTokens)
		}

		if chunk.Signal != nil {
			sig := &TokenSignal{
				Confidence: chunk.Signal.Confidence,
				Entropy:    chunk.Signal.Entropy,
				TopGap:     chunk.Signal.TopGap,
			}
			for _, tc := range chunk.Signal.TopK {
				sig.TopK = append(sig.TopK, TokenCandidate{
					ID:   tc.Id,
					Text: tc.Text,
					Prob: tc.Prob,
				})
			}
			sc.Signal = sig
		}

		streamChan <- sc

		if chunk.Done {
			return nil
		}
	}
}

var _ AIClient = (*GRPCLLMClient)(nil)
var _ StreamingAIClient = (*GRPCLLMClient)(nil)
