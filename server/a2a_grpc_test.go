package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/auth"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// "Harness is a plugin that speaks grpc A2A"

// a2aOverGRPCAsked is a node served as its port serves it, HTTP/2 without TLS
// beside HTTP/1, and a2a-go's gRPC client of its A2AService.
func a2aOverGRPCAsked(t *testing.T) (a2apb.A2AServiceClient, map[auth.Level]string) {
	t.Helper()
	held := filepath.Join(t.TempDir(), "held")
	require.NoError(t, os.WriteFile(held, nil, 0o600))
	srv, tokens, _ := nodeAgentOverA2A(t, held)
	node := httptest.NewUnstartedServer(srv.served)
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	node.Config.Protocols = protocols
	node.Start()
	t.Cleanup(node.Close)
	conn, err := grpc.NewClient(strings.TrimPrefix(node.URL, "http://"), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return a2apb.NewA2AServiceClient(conn), tokens
}

// as is ctx carrying token as gRPC carries a credential: the authorization
// metadata, which is the HTTP header the gate reads.
func asOverGRPC(token string) context.Context {
	return metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+token, "a2a-version", "1.0")
}

func saidOverGRPC(text string) *a2apb.SendMessageRequest {
	return &a2apb.SendMessageRequest{
		Message:       &a2apb.Message{MessageId: "m1", Role: a2apb.Role_ROLE_USER, Parts: []*a2apb.Part{{Content: &a2apb.Part_Text{Text: text}}}},
		Configuration: &a2apb.SendMessageConfiguration{ReturnImmediately: true},
	}
}

func TestTheNodeAgentIsSpokenToOverGRPC(t *testing.T) {
	client, tokens := a2aOverGRPCAsked(t)
	root := asOverGRPC(tokens[auth.LevelRoot])

	answered, err := client.SendMessage(root, saidOverGRPC("hi"))
	require.NoError(t, err)
	task := answered.GetTask()
	require.NotNil(t, task, "a turn is a Task")
	assert.Equal(t, a2apb.TaskState_TASK_STATE_SUBMITTED, task.GetStatus().GetState())

	// The Task it made is the one HTTP+JSON and JSON-RPC answer by that id.
	again, err := client.GetTask(root, &a2apb.GetTaskRequest{Id: task.GetId()})
	require.NoError(t, err)
	assert.Equal(t, task.GetId(), again.GetId())
	assert.Equal(t, task.GetContextId(), again.GetContextId())
}

func TestA2AOverGRPCGoesThroughTheGate(t *testing.T) {
	client, tokens := a2aOverGRPCAsked(t)

	// Nobody: the gate turns it away before A2A is asked anything.
	_, err := client.SendMessage(context.Background(), saidOverGRPC("hi"))
	require.Error(t, err)
	assert.Contains(t, []codes.Code{codes.Unauthenticated, codes.PermissionDenied, codes.Unknown, codes.Internal}, status.Code(err), err)

	// A2A is ROOT's: a USER is not served it.
	_, err = client.SendMessage(asOverGRPC(tokens[auth.LevelUser]), saidOverGRPC("hi"))
	require.Error(t, err)

	// A tenant no agent is served at does not exist, as over the other bindings (§3.3.2).
	request := saidOverGRPC("hi")
	request.Tenant = "Rowan"
	_, err = client.SendMessage(asOverGRPC(tokens[auth.LevelRoot]), request)
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err), err)
	assert.Contains(t, err.Error(), "no agent is served at tenant Rowan")
}

// The card lists gRPC where am.toml says it is reached, and nowhere else: the
// node serves it on its own port, and whether its front door passes HTTP/2 is
// the deployment's to say.
func TestTheCardListsGRPCWhereItIsReached(t *testing.T) {
	srv, _, _ := nodeAgentOverA2A(t, "")
	bindings := func() string {
		w := httptest.NewRecorder()
		srv.served.ServeHTTP(w, httptest.NewRequest(http.MethodGet, agentCardPath, nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		return w.Body.String()
	}
	assert.NotContains(t, bindings(), `"GRPC"`)

	appcfg.Set("a2a.grpc", "node.example:443")
	t.Cleanup(func() { appcfg.Set("a2a.grpc", "") })
	card := bindings()
	assert.Contains(t, card, `"GRPC"`)
	assert.Contains(t, card, "node.example:443")
}
