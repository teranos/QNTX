package grpc

import (
	"context"
	"fmt"
	"iter"
	"net"
	"testing"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// "Harness is a plugin that speaks grpc A2A" (ADR-059).

// harnessPlugin is a Go plugin that runs a harness: each turn is answered
// with what was said.
type harnessPlugin struct {
	*mockPlugin
}

func (p *harnessPlugin) Harness() a2asrv.RequestHandler {
	return a2asrv.NewHandler(a2asrv.AgentExecutorFunc(func(_ context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2asdk.Event, error] {
		return func(yield func(a2asdk.Event, error) bool) {
			yield(&a2asdk.Task{
				ID: ec.TaskID, ContextID: ec.ContextID,
				Status:    a2asdk.TaskStatus{State: a2asdk.TaskStateCompleted},
				Artifacts: []*a2asdk.Artifact{{ID: "echo", Parts: ec.Message.Parts}},
			}, nil)
		}
	}))
}

// A plugin that runs a harness says so at Initialize, and one that runs none
// does not.
func TestPluginServer_APluginRunningAHarnessIsAHarnessProvider(t *testing.T) {
	runs := NewPluginServer(&harnessPlugin{mockPlugin: newMockPlugin()}, zaptest.NewLogger(t).Sugar())
	resp, err := runs.Initialize(context.Background(), &protocol.InitializeRequest{})
	require.NoError(t, err)
	assert.True(t, resp.GetHarnessProvider())

	none := NewPluginServer(newMockPlugin(), zaptest.NewLogger(t).Sugar())
	resp, err = none.Initialize(context.Background(), &protocol.InitializeRequest{})
	require.NoError(t, err)
	assert.False(t, resp.GetHarnessProvider())
}

// Its harness answers as A2A's own A2AService on the plugin's port, the one
// connection core holds to it, and answers the node alone: a call presents
// the token the node handed the plugin at Initialize.
func TestPluginServer_ServesThePluginsHarnessAsA2AService(t *testing.T) {
	server := NewPluginServer(&harnessPlugin{mockPlugin: newMockPlugin()}, zaptest.NewLogger(t).Sugar())

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

	// "A2A-Version": the version spoken, which a2a-go's own client says itself.
	asking := metadata.AppendToOutgoingContext(ctx, "a2a-version", string(a2asdk.Version))
	turn := &a2apb.SendMessageRequest{
		Message: &a2apb.Message{MessageId: "m1", Role: a2apb.Role_ROLE_USER, Parts: []*a2apb.Part{{Content: &a2apb.Part_Text{Text: "how is the harvest?"}}}},
	}

	// Before the node initialized it, the plugin answers no turn.
	require.Eventually(t, func() bool {
		_, err = a2apb.NewA2AServiceClient(conn).SendMessage(asking, turn)
		return status.Code(err) == codes.Unauthenticated
	}, 10*time.Second, 50*time.Millisecond, "the plugin's A2AService answered a caller before the node handed it a token")

	_, err = server.Initialize(ctx, &protocol.InitializeRequest{AuthToken: "the-plugins-token"})
	require.NoError(t, err)

	// Whoever else reaches the port is refused: no token, and another's.
	_, err = a2apb.NewA2AServiceClient(conn).SendMessage(asking, turn)
	assert.Equal(t, codes.Unauthenticated, status.Code(err), "a call presenting no token ran a turn")
	_, err = a2apb.NewA2AServiceClient(PresentingHarnessToken(conn, "another-token")).SendMessage(asking, turn)
	assert.Equal(t, codes.Unauthenticated, status.Code(err), "a call presenting another token ran a turn")
	streamed, err := a2apb.NewA2AServiceClient(conn).SendStreamingMessage(asking, turn)
	if err == nil {
		_, err = streamed.Recv()
	}
	assert.Equal(t, codes.Unauthenticated, status.Code(err), "a streamed call presenting no token ran a turn")

	answered, err := a2apb.NewA2AServiceClient(PresentingHarnessToken(conn, "the-plugins-token")).SendMessage(asking, turn)
	require.NoError(t, err)
	task := answered.GetTask()
	require.NotNil(t, task)
	assert.Equal(t, a2apb.TaskState_TASK_STATE_COMPLETED, task.GetStatus().GetState())
	assert.Equal(t, "how is the harvest?", task.GetArtifacts()[0].GetParts()[0].GetText())
}
