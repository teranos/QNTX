//go:build e2e

package services

// The node's own LLM path — LLMServer, its queue and weave, and the
// GRPCLLMClient the prompt handlers call — against a running LLM provider
// plugin that reports how it served a call (the otari provider,
// github.com/teranos/otari-qntx). Run:
//
//	QNTX_LLM_E2E_ADDR=127.0.0.1:50140 QNTX_LLM_E2E_PROVIDER=otari \
//	QNTX_LLM_E2E_MODEL=fast QNTX_LLM_E2E_CONFIG='{"otari_url":"...","api_key_file":"..."}' \
//	go test -tags 'e2e quickdev' ./plugin/grpc/services -run E2E -v

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/ai/provider"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func e2eEnv(t *testing.T, name string) string {
	t.Helper()
	v := os.Getenv(name)
	if v == "" {
		t.Skipf("%s is not set", name)
	}
	return v
}

func TestE2E_TheServedReportReachesTheNodesCallers(t *testing.T) {
	addr := e2eEnv(t, "QNTX_LLM_E2E_ADDR")
	name := e2eEnv(t, "QNTX_LLM_E2E_PROVIDER")
	model := e2eEnv(t, "QNTX_LLM_E2E_MODEL")
	var pluginConfig map[string]string
	require.NoError(t, json.Unmarshal([]byte(e2eEnv(t, "QNTX_LLM_E2E_CONFIG")), &pluginConfig))

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	init, err := protocol.NewDomainPluginServiceClient(conn).Initialize(ctx, &protocol.InitializeRequest{Config: pluginConfig})
	require.NoError(t, err)
	require.True(t, init.LlmProvider)

	logger := zaptest.NewLogger(t).Sugar()
	store := &weaveStore{woven: make(chan *types.AsCommand, 8)}
	router := NewLLMServer(config.LLMConfig{MaxConcurrent: 2, MaxCallsPerMinute: 1000}, store, logger)
	router.RegisterProvider(name, protocol.NewLLMServiceClient(conn))
	client := provider.NewGRPCLLMClient(router, name)

	resp, err := client.Chat(ctx, provider.ChatRequest{
		Model:    &model,
		Messages: []provider.Message{provider.NewTextMessage("user", "Say hello.")},
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Served)
	t.Logf("served: %+v (cost %v)", *resp.Served, *resp.Served.CostUSD)
	assert.Equal(t, name, resp.Served.RequestedProvider)
	assert.Equal(t, model, resp.Served.RequestedModel)
	assert.NotEmpty(t, resp.Served.UpstreamProvider)
	assert.NotEmpty(t, resp.Served.ExecutedModel)
	assert.NotEmpty(t, resp.Served.RequestID)
	assert.Equal(t, resp.Served.ExecutedModel, resp.Model)

	woven := <-store.woven
	assert.Equal(t, []string{"model:" + resp.Served.ExecutedModel}, woven.Subjects)
	assert.Equal(t, model, woven.Attributes["requested_model"])
	assert.Equal(t, resp.Served.UpstreamProvider, woven.Attributes["upstream_provider"])
	assert.Equal(t, resp.Served.RequestID, woven.Attributes["request_id"])
	assert.Equal(t, *resp.Served.CostUSD, woven.Attributes["cost_usd"])

	chunks := make(chan provider.StreamChunk, 64)
	streamer := client.(provider.StreamingAIClient)
	go func() {
		assert.NoError(t, streamer.ChatStreaming(ctx, provider.ChatRequest{
			Model:    &model,
			Messages: []provider.Message{provider.NewTextMessage("user", "Say hello.")},
		}, chunks))
	}()
	var final provider.StreamChunk
	for chunk := range chunks {
		if chunk.Done {
			final = chunk
		}
	}
	require.NotNil(t, final.Served)
	assert.Equal(t, model, final.Served.RequestedModel)
	assert.Equal(t, resp.Served.UpstreamProvider, final.Served.UpstreamProvider)
	assert.NotEqual(t, resp.Served.RequestID, final.Served.RequestID, "each call has its own request id")
}
