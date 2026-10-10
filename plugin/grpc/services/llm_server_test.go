package services

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
)

// weaveStore keeps what the LLM server weaves; nothing else is asked of it.
type weaveStore struct {
	ats.AttestationStore
	woven chan *types.AsCommand
}

func (w *weaveStore) GenerateAndCreateAttestation(_ context.Context, cmd *types.AsCommand) (*types.As, error) {
	w.woven <- cmd
	return &types.As{ID: "AS-woven"}, nil
}

func newTestLLMServer(logger *zap.SugaredLogger) (*LLMServer, *weaveStore) {
	store := &weaveStore{woven: make(chan *types.AsCommand, 8)}
	return NewLLMServer(config.LLMConfig{MaxConcurrent: 2, MaxCallsPerMinute: 1000}, store, logger), store
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
	srv, _ := newTestLLMServer(zaptest.NewLogger(t).Sugar())

	_, err := srv.Chat(context.Background(), &protocol.LLMChatRequest{
		UserPrompt: "hello",
		Provider:   "openrouter",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `LLM provider "openrouter" is not registered`)
}

func TestLLMServer_ExplicitProvider(t *testing.T) {
	// The weave is written after the call returns, so what it logs may come
	// after the test has ended.
	srv, store := newTestLLMServer(zap.NewNop().Sugar())

	srv.RegisterProvider("a", &stubLLMClient{chatResp: &protocol.LLMChatResponse{Content: "from-a"}})
	srv.RegisterProvider("b", &stubLLMClient{chatResp: &protocol.LLMChatResponse{
		Content: "from-b", Model: "test-model", TotalTokens: 42,
	}})

	resp, err := srv.Chat(context.Background(), &protocol.LLMChatRequest{
		UserPrompt: "test",
		Provider:   "b",
	})
	require.NoError(t, err)
	assert.Equal(t, "from-b", resp.Content)
	assert.Equal(t, "test-model", resp.Model)
	assert.Equal(t, int32(42), resp.TotalTokens)

	woven := <-store.woven
	assert.Equal(t, []string{"b"}, woven.Actors)
	assert.Equal(t, "from-b", woven.Attributes["text"])
}

// "nil is nil"
func TestLLMServer_ACallNamingNoProviderIsRefused(t *testing.T) {
	srv, _ := newTestLLMServer(zaptest.NewLogger(t).Sugar())
	srv.RegisterProvider("first", &stubLLMClient{chatResp: &protocol.LLMChatResponse{Content: "first"}})

	_, err := srv.Chat(context.Background(), &protocol.LLMChatRequest{UserPrompt: "test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `LLM provider "" is not registered`)
}

// "nil is nil"
func TestLLMServer_AnUnknownProviderIsRefused(t *testing.T) {
	srv, _ := newTestLLMServer(zaptest.NewLogger(t).Sugar())
	srv.RegisterProvider("openrouter", &stubLLMClient{chatResp: &protocol.LLMChatResponse{Content: "from openrouter"}})

	_, err := srv.Chat(context.Background(), &protocol.LLMChatRequest{
		UserPrompt: "test",
		Provider:   "nonexistent",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `LLM provider "nonexistent" is not registered`)
}
