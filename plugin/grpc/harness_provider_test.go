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
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
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
// connection core holds to it.
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
	var answered *a2apb.SendMessageResponse
	require.Eventually(t, func() bool {
		answered, err = a2apb.NewA2AServiceClient(conn).SendMessage(asking, &a2apb.SendMessageRequest{
			Message: &a2apb.Message{MessageId: "m1", Role: a2apb.Role_ROLE_USER, Parts: []*a2apb.Part{{Content: &a2apb.Part_Text{Text: "how is the harvest?"}}}},
		})
		if err != nil {
			t.Log(err)
		}
		return err == nil
	}, 10*time.Second, 50*time.Millisecond, "the plugin's A2AService did not answer")
	task := answered.GetTask()
	require.NotNil(t, task)
	assert.Equal(t, a2apb.TaskState_TASK_STATE_COMPLETED, task.GetStatus().GetState())
	assert.Equal(t, "how is the harvest?", task.GetArtifacts()[0].GetParts()[0].GetText())
}
