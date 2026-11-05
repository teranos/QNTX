package server

import (
	"context"
	"encoding/json"
	"io"
	"iter"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// anotherAgent is an agent a2a-go serves, which answers each message with a
// Task completed, its artifact the message's text, at the tenant it was sent.
type anotherAgent struct {
	tenant []string
	bearer []string
}

func (a *anotherAgent) executor() a2asrv.AgentExecutor {
	return a2asrv.AgentExecutorFunc(func(_ context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2asdk.Event, error] {
		return func(yield func(a2asdk.Event, error) bool) {
			a.tenant = append(a.tenant, ec.Tenant)
			yield(&a2asdk.Task{
				ID: ec.TaskID, ContextID: ec.ContextID,
				Status:    a2asdk.TaskStatus{State: a2asdk.TaskStateCompleted},
				History:   []*a2asdk.Message{ec.Message},
				Artifacts: []*a2asdk.Artifact{{ID: "echo", Parts: ec.Message.Parts}},
			}, nil)
		}
	})
}

func a2aCalledOver(t *testing.T, srv *QNTXServer, token, method, rpc string, sent map[string]any) (int, map[string]any) {
	t.Helper()
	path := "/api/a2a/" + rpc
	r := asBearer(method, path, token)
	if method == http.MethodGet || method == http.MethodDelete {
		query := r.URL.Query()
		for name, value := range sent {
			query.Set(name, value.(string))
		}
		r.URL.RawQuery = query.Encode()
	} else {
		body, err := json.Marshal(sent)
		require.NoError(t, err)
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		r.ContentLength = int64(len(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	var answered map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answered), w.Body.String())
	return w.Code, answered
}

// SendMessage and GetTask, spoken to an agent a peer serves at a tenant, as
// the person asking here; what the peer answered is the spec's response.
func TestAPeersAgentIsSpokenToAsTheSpecSays(t *testing.T) {
	agent := &anotherAgent{}
	atTenant := a2asrv.NewTenantRESTHandler("/{*}", a2asrv.NewHandler(agent.executor()))
	peerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent.bearer = append(agent.bearer, r.Header.Get("Authorization"))
		http.StripPrefix("/a2a", atTenant).ServeHTTP(w, r)
	}))
	t.Cleanup(peerServer.Close)
	namedNode(t)
	srv, tokens := pluginServingServer(t, "fake")
	keyedNode(srv)
	require.NoError(t, srv.authHandler.DeclarePeer("orchardnode", "did:key:z6Mksjm1puegvWqoRsYxu9Xz4Snu1WLHFce8FGEVKhWEtoX4", []string{"orchard"}, peerServer.URL, peerServer.URL))
	root := tokens[auth.LevelRoot]

	code, sent := a2aCalledOver(t, srv, root, http.MethodPost, "SendMessage", map[string]any{
		"peer": "orchardnode", "tenant": "orchard",
		"message": `{"message_id": "m1", "role": "ROLE_USER", "parts": [{"text": "how is the harvest?"}]}`,
	})
	require.Equal(t, http.StatusOK, code, sent)
	task, isTask := sent["task"].(map[string]any)
	require.True(t, isTask, sent)
	assert.Equal(t, "TASK_STATE_COMPLETED", task["status"].(map[string]any)["state"])
	artifact := task["artifacts"].([]any)[0].(map[string]any)
	assert.Equal(t, "how is the harvest?", artifact["parts"].([]any)[0].(map[string]any)["text"])
	require.Len(t, agent.tenant, 1)
	assert.Equal(t, "orchard", agent.tenant[0])
	require.NotEmpty(t, agent.bearer)
	assert.True(t, strings.HasPrefix(agent.bearer[0], "Bearer "), "the peer was not spoken to as the person")

	code, refused := a2aCalledOver(t, srv, root, http.MethodGet, "GetTask", map[string]any{"peer": "orchardnode", "tenant": "orchard", "id": "no-such-task"})
	assert.NotEqual(t, http.StatusOK, code, refused)

	code, refused = a2aCalledOver(t, srv, root, http.MethodPost, "SendMessage", map[string]any{"peer": "nowhere", "message": `{}`})
	assert.Equal(t, http.StatusNotFound, code, refused)
}

// Every sigil of a2a is an operation of the pinned A2AService, its request's
// fields what it takes beside peer.
func TestEachA2ASigilIsAnOperationOfThePinnedSpec(t *testing.T) {
	srv, _ := pluginServingServer(t, "fake")
	signum, err := srv.a2aClientSignum()
	require.NoError(t, err)
	pinned, err := a2aSpec()
	require.NoError(t, err)
	for _, held := range signum.GetSigils() {
		method := pinned.service.Methods().ByName(protoreflect.Name(held.GetName()))
		require.NotNil(t, method, held.GetName())
		assert.Equal(t, string(method.Output().FullName()), held.GetAnswer(), held.GetName())
		assert.NotEmpty(t, held.GetDoes(), held.GetName())
		fields := method.Input().Fields()
		require.Len(t, held.GetTakes(), fields.Len()+1, held.GetName())
		for i := 0; i < fields.Len(); i++ {
			assert.Equal(t, string(fields.Get(i).Name()), held.GetTakes()[i+1].GetName(), held.GetName())
		}
	}
	assert.Len(t, signum.GetSigils(), pinned.service.Methods().Len()-2, "every operation but the two that stream")
}
