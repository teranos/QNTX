package grpc

import (
	"context"
	"fmt"
	"iter"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/plugin/harness"
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

// served is server serving on loopback, and a connection to it.
func served(t *testing.T, server *PluginServer) (context.Context, *grpc.ClientConn) {
	t.Helper()
	free, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := free.Addr().(*net.TCPAddr).Port
	require.NoError(t, free.Close())

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = server.Serve(ctx, fmt.Sprintf("127.0.0.1:%d", port)) }()

	conn, err := grpc.NewClient(fmt.Sprintf("127.0.0.1:%d", port), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	// "A2A-Version": the version spoken, which a2a-go's own client says itself.
	return metadata.AppendToOutgoingContext(ctx, "a2a-version", string(a2asdk.Version)), conn
}

var aTurn = &a2apb.SendMessageRequest{
	Message: &a2apb.Message{MessageId: "m1", Role: a2apb.Role_ROLE_USER, Parts: []*a2apb.Part{{Content: &a2apb.Part_Text{Text: "how is the harvest?"}}}},
}

// Its harness answers as A2A's own A2AService on the plugin's port, the one
// connection core holds to it, and answers the node that started it alone: a
// call presents the secret that node handed the plugin's process.
func TestPluginServer_ServesThePluginsHarnessAsA2AService(t *testing.T) {
	t.Setenv(HarnessSecretEnv, "the-launch-secret")
	server := NewPluginServer(&harnessPlugin{mockPlugin: newMockPlugin()}, zaptest.NewLogger(t).Sugar())
	_, handedOn := os.LookupEnv(HarnessSecretEnv)
	assert.False(t, handedOn, "what the plugin starts would be handed the secret of its launch")
	asking, conn := served(t, server)

	// Whoever else reaches the port is refused: no secret, and another.
	require.Eventually(t, func() bool {
		_, err := a2apb.NewA2AServiceClient(conn).SendMessage(asking, aTurn)
		return status.Code(err) == codes.Unauthenticated
	}, 10*time.Second, 50*time.Millisecond, "a call presenting no secret ran a turn")
	_, err := a2apb.NewA2AServiceClient(PresentingHarnessSecret(conn, "another-secret")).SendMessage(asking, aTurn)
	assert.Equal(t, codes.Unauthenticated, status.Code(err), "a call presenting another secret ran a turn")
	streamed, err := a2apb.NewA2AServiceClient(conn).SendStreamingMessage(asking, aTurn)
	if err == nil {
		_, err = streamed.Recv()
	}
	assert.Equal(t, codes.Unauthenticated, status.Code(err), "a streamed call presenting no secret ran a turn")

	// Initialize is answered to whoever reaches the port, so what it carries
	// is nothing the harness knows the node by.
	_, err = server.Initialize(asking, &protocol.InitializeRequest{AuthToken: "claimed-by-the-first-to-ask"})
	require.NoError(t, err)
	_, err = a2apb.NewA2AServiceClient(PresentingHarnessSecret(conn, "claimed-by-the-first-to-ask")).SendMessage(asking, aTurn)
	assert.Equal(t, codes.Unauthenticated, status.Code(err), "whoever initialized the plugin first chose what its harness answers to")

	// The node's own token changes as its config is saved; the launch's
	// secret does not.
	_, err = server.Initialize(asking, &protocol.InitializeRequest{AuthToken: "minted-again"})
	require.NoError(t, err)
	answered, err := a2apb.NewA2AServiceClient(PresentingHarnessSecret(conn, "the-launch-secret")).SendMessage(asking, aTurn)
	require.NoError(t, err)
	task := answered.GetTask()
	require.NotNil(t, task)
	assert.Equal(t, a2apb.TaskState_TASK_STATE_COMPLETED, task.GetStatus().GetState())
	assert.Equal(t, "how is the harvest?", task.GetArtifacts()[0].GetParts()[0].GetText())
}

// A plugin no node started was handed no secret, and its harness answers
// nobody.
func TestPluginServer_APluginNoNodeStartedAnswersNoTurn(t *testing.T) {
	asking, conn := served(t, NewPluginServer(&harnessPlugin{mockPlugin: newMockPlugin()}, zaptest.NewLogger(t).Sugar()))
	require.Eventually(t, func() bool {
		_, err := a2apb.NewA2AServiceClient(PresentingHarnessSecret(conn, "")).SendMessage(asking, aTurn)
		return status.Code(err) == codes.Unauthenticated
	}, 10*time.Second, 50*time.Millisecond, "a plugin handed no secret ran a turn")
}

// waitingPlugin runs a harness whose turn says nothing and runs until it is
// stopped.
type waitingPlugin struct {
	*mockPlugin
	started chan struct{}
	stopped chan struct{}
	once    sync.Once
}

func (p *waitingPlugin) Harness() a2asrv.RequestHandler {
	return a2asrv.NewHandler(a2asrv.AgentExecutorFunc(func(ctx context.Context, _ *a2asrv.ExecutorContext) iter.Seq2[a2asdk.Event, error] {
		return func(func(a2asdk.Event, error) bool) {
			running, stop := context.WithCancel(ctx)
			defer stop()
			harness.StoppedWhenTheCallerLeaves(ctx, stop)
			first := false
			p.once.Do(func() { first = true; close(p.started) })
			<-running.Done()
			if first {
				close(p.stopped)
			}
		}
	}))
}

// A caller that leaves before it was told which Task its turn is has no Task
// to cancel: the plugin's server stops the run its stream asked for.
func TestPluginServer_ACallerLeavingBeforeAnyEventStopsItsTurn(t *testing.T) {
	t.Setenv(HarnessSecretEnv, "the-launch-secret")
	runs := &waitingPlugin{mockPlugin: newMockPlugin(), started: make(chan struct{}), stopped: make(chan struct{})}
	asking, conn := served(t, NewPluginServer(runs, zaptest.NewLogger(t).Sugar()))

	client := a2apb.NewA2AServiceClient(PresentingHarnessSecret(conn, "the-launch-secret"))
	calling, leave := context.WithCancel(asking)
	defer leave()
	require.Eventually(t, func() bool {
		select {
		case <-runs.started:
			return true
		default:
		}
		// A stream the server was not yet up for ends at once, and is asked again.
		streamed, err := client.SendStreamingMessage(calling, aTurn)
		if err == nil {
			go func() { _, _ = streamed.Recv() }()
		}
		time.Sleep(200 * time.Millisecond)
		return false
	}, 10*time.Second, 10*time.Millisecond, "the plugin's harness was not asked for a turn")

	select {
	case <-runs.stopped:
		t.Fatal("the turn was stopped while its caller listened")
	case <-time.After(200 * time.Millisecond):
	}
	leave()
	select {
	case <-runs.stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("the turn runs on in the plugin after its caller left")
	}
}

// The secret of a launch is in the environment of the process the node
// starts, and is what the node's connection to it presents.
func TestPluginManager_HandsEachLaunchItsHarnessSecret(t *testing.T) {
	dir := t.TempDir()
	handed := filepath.Join(dir, "handed")
	binary := filepath.Join(dir, "qntx-waits-plugin")
	require.NoError(t, os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s' \"$"+HarnessSecretEnv+"\" > '"+handed+"'\necho QNTX_PLUGIN_PORT=1\nexec sleep 60\n"), 0o755))

	logger := zaptest.NewLogger(t).Sugar()
	manager := NewPluginManager(logger, logger, "")
	manager.logDir = filepath.Join(dir, "logs")
	launched, _, err := manager.launchPlugin(context.Background(), PluginConfig{Name: "waits", Enabled: true, Source: LaunchBinary(binary)}, 1)
	require.NoError(t, err)
	t.Cleanup(func() { _ = launched.abandon() })

	require.Len(t, launched.harnessSecret, 64)
	var written []byte
	require.Eventually(t, func() bool {
		written, err = os.ReadFile(handed)
		return err == nil && len(written) > 0
	}, 5*time.Second, 20*time.Millisecond, "the plugin's process was handed no secret")
	assert.Equal(t, launched.harnessSecret, string(written))
}
