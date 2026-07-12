package provider

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
)

// GRPCLLMClient adapts the gRPC LLMService to the AIClient interface.
// GRPCLLMClient adapts the gRPC LLMService to the AIClient interface.
type GRPCLLMClient struct {
	router   GRPCLLMRouter
	provider string
}

// GRPCLLMRouter is the interface for routing LLM requests to gRPC providers.
// Satisfied by grpc.LLMServer.
type GRPCLLMRouter interface {
	Chat(ctx context.Context, req *protocol.LLMChatRequest) (*protocol.LLMChatResponse, error)
	StreamChatClient(ctx context.Context, req *protocol.LLMChatRequest) (protocol.LLMService_StreamChatClient, func(), error)
}

// NewGRPCLLMClient creates an AIClient that routes through the gRPC LLM service.
// The returned client also implements StreamingAIClient — use type assertion to access streaming.
func NewGRPCLLMClient(router GRPCLLMRouter, provider string) AIClient {
	return &GRPCLLMClient{router: router, provider: provider}
}

// buildGRPCRequest maps a ChatRequest to a proto LLMChatRequest.
func (c *GRPCLLMClient) buildGRPCRequest(req ChatRequest) *protocol.LLMChatRequest {
	grpcReq := &protocol.LLMChatRequest{
		SystemPrompt: req.SystemPrompt,
		UserPrompt:   req.UserPrompt,
		Provider:     c.provider,
	}

	// Multi-turn: populate messages from ChatRequest
	for _, m := range req.Messages {
		grpcReq.Messages = append(grpcReq.Messages, &protocol.ChatMessage{
			Role:    m.Role,
			Content: m.TextContent(),
		})
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

	return grpcReq
}

// Chat implements AIClient by forwarding to the gRPC LLM router.
func (c *GRPCLLMClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	grpcReq := c.buildGRPCRequest(req)

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
	grpcReq := c.buildGRPCRequest(req)

	t0 := time.Now()
	fmt.Printf("[grpc-llm] calling gRPC StreamChat...\n")
	stream, release, err := c.router.StreamChatClient(ctx, grpcReq)
	fmt.Printf("[grpc-llm] gRPC call returned in %dms\n", time.Since(t0).Milliseconds())
	if err != nil {
		return errors.Wrapf(err, "gRPC LLM stream chat via provider %s failed", c.provider)
	}

	defer release()
	defer close(streamChan)

	tFirstRecv := time.Now()

	for {
		chunk, err := stream.Recv()
		if tFirstRecv != (time.Time{}) {
			fmt.Printf("[grpc-llm] first chunk received %dms after gRPC call\n", time.Since(tFirstRecv).Milliseconds())
			tFirstRecv = time.Time{}
		}
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
				Confidence:       chunk.Signal.Confidence,
				Entropy:          chunk.Signal.Entropy,
				TopGap:           chunk.Signal.TopGap,
				FullDistribution: chunk.Signal.FullDistribution,
			}
			for _, tc := range chunk.Signal.TopK {
				sig.TopK = append(sig.TopK, TokenCandidate{
					ID:   tc.Id,
					Text: tc.Text,
					Prob: tc.Prob,
				})
			}
			for _, stage := range chunk.Signal.SamplerStages {
				ss := SamplerStageSignal{
					Name:        stage.Name,
					ActiveCount: stage.ActiveCount,
					Top1Prob:    stage.Top1Prob,
					Entropy:     stage.Entropy,
				}
				for _, tc := range stage.TopK {
					ss.TopK = append(ss.TopK, TokenCandidate{
						ID:   tc.Id,
						Text: tc.Text,
						Prob: tc.Prob,
					})
				}
				sig.SamplerStages = append(sig.SamplerStages, ss)
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
