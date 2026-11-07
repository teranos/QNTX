package grpc

import (
	"context"
	"fmt"
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// streamingLLM serves the whole LLMService: a multi-turn request is answered
// token by token, the final chunk carrying the served report.
type streamingLLM struct {
	protocol.UnimplementedLLMServiceServer
	asked chan *protocol.LLMChatRequest
}

func (s *streamingLLM) StreamChat(req *protocol.LLMChatRequest, srv protocol.LLMService_StreamChatServer) error {
	s.asked <- req
	for _, token := range []string{"Hel", "lo"} {
		if err := srv.Send(&protocol.LLMChatChunk{Token: token, SignalOf: &protocol.LLMChatChunk_NoSignal{NoSignal: true}}); err != nil {
			return err
		}
	}
	return srv.Send(&protocol.LLMChatChunk{
		Done: true, Model: "good-model", TotalTokens: 17,
		SignalOf: &protocol.LLMChatChunk_NoSignal{NoSignal: true},
		Served:   &protocol.LLMServed{RequestedModel: req.Model, UpstreamProvider: "secondary", ExecutedModel: "good-model"},
	})
}

// llmServingPlugin is a Go plugin that serves its own LLMService.
type llmServingPlugin struct {
	*mockPlugin
	llm *streamingLLM
}

func (p *llmServingPlugin) LLMService() protocol.LLMServiceServer { return p.llm }

// A plugin serving the whole LLMService is an LLM provider to the node.
func TestPluginServer_APluginServingItsOwnLLMServiceIsAnLLMProvider(t *testing.T) {
	p := &llmServingPlugin{mockPlugin: newMockPlugin(), llm: &streamingLLM{asked: make(chan *protocol.LLMChatRequest, 8)}}
	server := NewPluginServer(p, zaptest.NewLogger(t).Sugar())

	resp, err := server.Initialize(context.Background(), &protocol.InitializeRequest{})
	require.NoError(t, err)
	assert.True(t, resp.LlmProvider)
}

// Its own server is the one the node reaches: StreamChat answers, the
// messages field arrives whole, and the served report rides the final chunk.
func TestPluginServer_ServesThePluginsOwnLLMService(t *testing.T) {
	p := &llmServingPlugin{mockPlugin: newMockPlugin(), llm: &streamingLLM{asked: make(chan *protocol.LLMChatRequest, 8)}}
	server := NewPluginServer(p, zaptest.NewLogger(t).Sugar())

	free, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := free.Addr().(*net.TCPAddr).Port
	require.NoError(t, free.Close())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = server.Serve(ctx, fmt.Sprintf("127.0.0.1:%d", port)) }()

	conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", port), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer conn.Close()

	var stream protocol.LLMService_StreamChatClient
	require.Eventually(t, func() bool {
		stream, err = protocol.NewLLMServiceClient(conn).StreamChat(ctx, &protocol.LLMChatRequest{
			Provider: "mock", Model: "fast",
			Messages: []*protocol.ChatMessage{{Role: "system", Content: "be brief"}, {Role: "user", Content: "hi"}},
		})
		if err != nil {
			return false
		}
		_, err = stream.Header()
		return err == nil
	}, 5*time.Second, 20*time.Millisecond)

	var text string
	var final *protocol.LLMChatChunk
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		text += chunk.Token
		if chunk.Done {
			final = chunk
		}
	}
	assert.Equal(t, "Hello", text)
	require.NotNil(t, final)
	assert.Equal(t, "secondary", final.GetServed().GetUpstreamProvider())
	assert.Equal(t, "fast", final.GetServed().GetRequestedModel())

	asked := <-p.llm.asked
	require.Len(t, asked.Messages, 2)
	assert.Equal(t, "be brief", asked.Messages[0].Content)
}
