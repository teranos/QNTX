package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/am"
	"github.com/teranos/errors"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
)

func newTestLLMServer(logger *zap.SugaredLogger) *LLMServer {
	return NewLLMServer(am.LLMConfig{MaxConcurrent: 2, MaxCallsPerMinute: 1000}, nil, logger)
}

// stubLLMClient implements protocol.LLMServiceClient for testing.
// Only Chat is wired; StreamChat returns unimplemented.
type stubLLMClient struct {
	chatResp *protocol.LLMChatResponse
	chatErr  error
}

func (s *stubLLMClient) Chat(ctx context.Context, in *protocol.LLMChatRequest, opts ...grpc.CallOption) (*protocol.LLMChatResponse, error) {
	return s.chatResp, s.chatErr
}

func (s *stubLLMClient) StreamChat(ctx context.Context, in *protocol.LLMChatRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[protocol.LLMChatChunk], error) {
	return nil, errors.New("StreamChat not implemented in test stub")
}

func TestLLMServer_NoProviders(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	srv := newTestLLMServer(logger)

	_, err := srv.Chat(context.Background(), &protocol.LLMChatRequest{
		UserPrompt: "hello",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no LLM providers registered")
}

func TestLLMServer_DefaultProvider(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	srv := newTestLLMServer(logger)

	stub := &stubLLMClient{
		chatResp: &protocol.LLMChatResponse{
			Content:     "world",
			Model:       "test-model",
			TotalTokens: 42,
		},
	}
	srv.RegisterProvider("openrouter", stub)

	resp, err := srv.Chat(context.Background(), &protocol.LLMChatRequest{
		UserPrompt: "hello",
		// provider empty → uses default
	})
	require.NoError(t, err)
	assert.Equal(t, "world", resp.Content)
	assert.Equal(t, "test-model", resp.Model)
	assert.Equal(t, int32(42), resp.TotalTokens)
}

func TestLLMServer_ExplicitProvider(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	srv := newTestLLMServer(logger)

	stubA := &stubLLMClient{
		chatResp: &protocol.LLMChatResponse{Content: "from-a"},
	}
	stubB := &stubLLMClient{
		chatResp: &protocol.LLMChatResponse{Content: "from-b"},
	}

	srv.RegisterProvider("a", stubA)
	srv.RegisterProvider("b", stubB)

	// Explicit provider=b
	resp, err := srv.Chat(context.Background(), &protocol.LLMChatRequest{
		UserPrompt: "test",
		Provider:   "b",
	})
	require.NoError(t, err)
	assert.Equal(t, "from-b", resp.Content)

	// Default should be "a" (first registered)
	resp, err = srv.Chat(context.Background(), &protocol.LLMChatRequest{
		UserPrompt: "test",
	})
	require.NoError(t, err)
	assert.Equal(t, "from-a", resp.Content)
}

func TestLLMServer_UnknownProviderFallsBackToDefault(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	srv := newTestLLMServer(logger)

	stub := &stubLLMClient{
		chatResp: &protocol.LLMChatResponse{Content: "from default"},
	}
	srv.RegisterProvider("openrouter", stub)

	resp, err := srv.Chat(context.Background(), &protocol.LLMChatRequest{
		UserPrompt: "test",
		Provider:   "nonexistent",
	})
	require.NoError(t, err)
	assert.Equal(t, "from default", resp.Content)
}

func TestLLMServer_FirstRegisteredIsDefault(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	srv := newTestLLMServer(logger)

	srv.RegisterProvider("first", &stubLLMClient{
		chatResp: &protocol.LLMChatResponse{Content: "first"},
	})
	srv.RegisterProvider("second", &stubLLMClient{
		chatResp: &protocol.LLMChatResponse{Content: "second"},
	})

	// Empty provider → default → "first"
	resp, err := srv.Chat(context.Background(), &protocol.LLMChatRequest{
		UserPrompt: "test",
	})
	require.NoError(t, err)
	assert.Equal(t, "first", resp.Content)
}
