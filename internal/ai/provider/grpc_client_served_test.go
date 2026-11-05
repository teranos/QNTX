package provider

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"google.golang.org/grpc"
)

// servingRouter answers as a gateway provider does: what was asked beside
// what served it.
type servingRouter struct {
	served *protocol.LLMServed
}

func (r *servingRouter) Chat(_ context.Context, req *protocol.LLMChatRequest) (*protocol.LLMChatResponse, error) {
	return &protocol.LLMChatResponse{Content: "hi", Model: "good-model", TotalTokens: 17, Served: r.served}, nil
}

func (r *servingRouter) StreamChatClient(_ context.Context, req *protocol.LLMChatRequest) (protocol.LLMService_StreamChatClient, func(), error) {
	return &cannedStream{chunks: []*protocol.LLMChatChunk{
		{Token: "hi", SignalOf: &protocol.LLMChatChunk_NoSignal{NoSignal: true}},
		{Done: true, Model: "good-model", TotalTokens: 17, SignalOf: &protocol.LLMChatChunk_NoSignal{NoSignal: true}, Served: r.served},
	}}, func() {}, nil
}

type cannedStream struct {
	grpc.ClientStream
	chunks []*protocol.LLMChatChunk
}

func (s *cannedStream) Recv() (*protocol.LLMChatChunk, error) {
	if len(s.chunks) == 0 {
		return nil, io.EOF
	}
	chunk := s.chunks[0]
	s.chunks = s.chunks[1:]
	return chunk, nil
}

func otariServed() *protocol.LLMServed {
	cost := "0.000023"
	return &protocol.LLMServed{
		RequestedProvider: "otari", RequestedModel: "fast",
		UpstreamProvider: "secondary", ExecutedModel: "good-model", ResponseModel: "fast",
		RequestId: "req-1", CostUsd: &cost, PricingSource: "deployment", AttemptCount: 2, Fallback: true,
	}
}

func TestGRPCLLMClient_ChatCarriesTheServedReport(t *testing.T) {
	client := NewGRPCLLMClient(&servingRouter{served: otariServed()}, "otari")

	resp, err := client.Chat(context.Background(), ChatRequest{UserPrompt: "hi"})
	require.NoError(t, err)

	require.NotNil(t, resp.Served)
	assert.Equal(t, "good-model", resp.Model)
	assert.Equal(t, "fast", resp.Served.RequestedModel)
	assert.Equal(t, "secondary", resp.Served.UpstreamProvider)
	assert.Equal(t, "good-model", resp.Served.ExecutedModel)
	assert.Equal(t, "req-1", resp.Served.RequestID)
	require.NotNil(t, resp.Served.CostUSD)
	assert.Equal(t, "0.000023", *resp.Served.CostUSD)
	assert.Equal(t, 2, resp.Served.AttemptCount)
	assert.True(t, resp.Served.Fallback)
}

func TestGRPCLLMClient_NoReportIsNoServed(t *testing.T) {
	client := NewGRPCLLMClient(&servingRouter{}, "gaze")

	resp, err := client.Chat(context.Background(), ChatRequest{UserPrompt: "hi"})
	require.NoError(t, err)
	assert.Nil(t, resp.Served)
}

func TestGRPCLLMClient_TheFinalChunkCarriesTheServedReport(t *testing.T) {
	client := NewGRPCLLMClient(&servingRouter{served: otariServed()}, "otari").(StreamingAIClient)
	chunks := make(chan StreamChunk, 4)

	require.NoError(t, client.ChatStreaming(context.Background(), ChatRequest{UserPrompt: "hi"}, chunks))

	var got []StreamChunk
	for chunk := range chunks {
		got = append(got, chunk)
	}
	require.Len(t, got, 2)
	assert.Nil(t, got[0].Served)
	require.NotNil(t, got[1].Served)
	assert.Equal(t, "secondary", got[1].Served.UpstreamProvider)
	assert.Equal(t, "req-1", got[1].Served.RequestID)
}
