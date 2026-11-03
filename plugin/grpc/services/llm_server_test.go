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
	errors "github.com/teranos/sacred-error"
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
	actors, err := woven.NamedActors()
	require.NoError(t, err)
	assert.Equal(t, []string{"b"}, actors)
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

// A provider's report of how a call was served is woven beside the model the
// provider answered with: the policy asked for, the upstream and model that
// served it, the gateway's request id and its cost, each under its own name.
func TestLLMServer_TheServedReportIsWovenBesideTheModel(t *testing.T) {
	srv, store := newTestLLMServer(zap.NewNop().Sugar())
	cost := "0.000023"
	srv.RegisterProvider("otari", &stubLLMClient{chatResp: &protocol.LLMChatResponse{
		Content: "hello", Model: "good-model", TotalTokens: 17,
		Served: &protocol.LLMServed{
			RequestedProvider: "otari",
			RequestedModel:    "fast",
			UpstreamProvider:  "secondary",
			ExecutedModel:     "good-model",
			ResponseModel:     "fast",
			RequestId:         "a445a48d-c606-4b70-bce6-04790f3705e2",
			CostUsd:           &cost,
			PricingSource:     "deployment",
			AttemptCount:      2,
			Fallback:          true,
		},
	}})

	_, err := srv.Chat(context.Background(), &protocol.LLMChatRequest{UserPrompt: "hi", Provider: "otari", Model: "fast"})
	require.NoError(t, err)

	woven := <-store.woven
	assert.Equal(t, []string{"model:good-model"}, woven.Subjects)
	assert.Equal(t, "otari", woven.Attributes["requested_provider"])
	assert.Equal(t, "fast", woven.Attributes["requested_model"])
	assert.Equal(t, "secondary", woven.Attributes["upstream_provider"])
	assert.Equal(t, "good-model", woven.Attributes["executed_model"])
	assert.Equal(t, "fast", woven.Attributes["response_model"])
	assert.Equal(t, "a445a48d-c606-4b70-bce6-04790f3705e2", woven.Attributes["request_id"])
	assert.Equal(t, "0.000023", woven.Attributes["cost_usd"])
	assert.Equal(t, "deployment", woven.Attributes["pricing_source"])
	assert.Equal(t, 2, woven.Attributes["attempt_count"])
	assert.Equal(t, true, woven.Attributes["fallback"])
}

// A provider that reports nothing past its model weaves no served fields: an
// absent report is not filled in with what was asked.
func TestLLMServer_NoServedReportWeavesNoServedFields(t *testing.T) {
	srv, store := newTestLLMServer(zap.NewNop().Sugar())
	srv.RegisterProvider("gaze", &stubLLMClient{chatResp: &protocol.LLMChatResponse{Content: "hi", Model: "Qwen"}})

	_, err := srv.Chat(context.Background(), &protocol.LLMChatRequest{UserPrompt: "hi", Provider: "gaze", Model: "Qwen"})
	require.NoError(t, err)

	woven := <-store.woven
	for _, key := range []string{"requested_provider", "requested_model", "upstream_provider", "executed_model", "response_model", "request_id", "cost_usd", "pricing_source", "attempt_count", "fallback"} {
		assert.NotContains(t, woven.Attributes, key)
	}
}
