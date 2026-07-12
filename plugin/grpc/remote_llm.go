package grpc

import (
	"context"

	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
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
	}, nil
}
