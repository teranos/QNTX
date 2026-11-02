package server

import (
	"context"
	"encoding/json"
	"io"
	"iter"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
)

// "Why wouldn't the agents talk to each other through A2A?"

// anotherAgent is an agent a2a-go serves, which answers each message with a
// Task completed, its artifact what it heard and the tenant it heard it at.
type anotherAgent struct {
	heard  []string
	tenant []string
	bearer []string
}

func (a *anotherAgent) executor() a2asrv.AgentExecutor {
	return a2asrv.AgentExecutorFunc(func(_ context.Context, ec *a2asrv.ExecutorContext) iter.Seq2[a2asdk.Event, error] {
		return func(yield func(a2asdk.Event, error) bool) {
			said := partsText(ec.Message.Parts)
			a.heard = append(a.heard, said)
			a.tenant = append(a.tenant, ec.Tenant)
			yield(&a2asdk.Task{
				ID: ec.TaskID, ContextID: ec.ContextID,
				Status:    a2asdk.TaskStatus{State: a2asdk.TaskStateCompleted},
				History:   []*a2asdk.Message{ec.Message},
				Artifacts: []*a2asdk.Artifact{{ID: "answer", Parts: a2asdk.ContentParts{a2asdk.NewTextPart("heard: " + said)}}},
			}, nil)
		}
	})
}

// allowingLoopback lets the node speak to httptest, which is on loopback.
func allowingLoopback(t *testing.T) {
	t.Helper()
	allowed := webhookAddress
	webhookAddress = func(net.IP) bool { return true }
	t.Cleanup(func() { webhookAddress = allowed })
}

func a2aAskedOver(t *testing.T, srv *QNTXServer, token string, asked map[string]string) (int, map[string]any) {
	t.Helper()
	body, err := json.Marshal(asked)
	require.NoError(t, err)
	r := asBearer(http.MethodPost, "/api/a2a/ask", token)
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	r.ContentLength = int64(len(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	var answered map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answered), w.Body.String())
	return w.Code, answered
}

// An agent a peer serves is asked of the peer, at its tenant, as the person
// asking here, and what it answered comes back with the Task it became.
func TestAnAgentAPeerServesIsAsked(t *testing.T) {
	agent := &anotherAgent{}
	handler := a2asrv.NewHandler(agent.executor())
	atTenant := a2asrv.NewTenantRESTHandler("/{*}", handler)
	peerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent.bearer = append(agent.bearer, r.Header.Get("Authorization"))
		http.StripPrefix("/a2a", atTenant).ServeHTTP(w, r)
	}))
	t.Cleanup(peerServer.Close)
	peer := &standInPeer{server: peerServer}
	namedNode(t)
	srv, tokens := pluginServingServer(t, "fake")
	keyedNode(srv)
	require.NoError(t, srv.authHandler.DeclarePeer("orchardnode", "did:key:z6Mksjm1puegvWqoRsYxu9Xz4Snu1WLHFce8FGEVKhWEtoX4", []string{"orchard"}, peer.server.URL, peer.server.URL))

	code, asked := a2aAskedOver(t, srv, tokens[auth.LevelRoot], map[string]string{"peer": "orchardnode", "tenant": "orchard", "says": "how is the harvest?"})
	require.Equal(t, http.StatusOK, code, asked)
	assert.Equal(t, "heard: how is the harvest?", asked["answer"])
	assert.Equal(t, "TASK_STATE_COMPLETED", asked["state"])
	assert.NotEmpty(t, asked["task"])
	assert.NotEmpty(t, asked["context"])
	assert.Equal(t, "the agent of orchard at orchardnode", asked["agent"])
	require.Len(t, agent.tenant, 1)
	assert.Equal(t, "orchard", agent.tenant[0], "the peer was not asked at the tenant")
	require.NotEmpty(t, agent.bearer)
	assert.True(t, strings.HasPrefix(agent.bearer[0], "Bearer "), "the peer was not asked as the person")

	code, refused := a2aAskedOver(t, srv, tokens[auth.LevelRoot], map[string]string{"peer": "orchardnode", "tenant": "garden", "says": "hi"})
	assert.Equal(t, http.StatusNotFound, code, refused)
	code, refused = a2aAskedOver(t, srv, tokens[auth.LevelRoot], map[string]string{"peer": "nowhere", "says": "hi"})
	assert.Equal(t, http.StatusNotFound, code, refused)
}

// The agent a card names is asked as no one, at the interface the card names.
func TestTheAgentACardNamesIsAsked(t *testing.T) {
	allowingLoopback(t)
	agent := &anotherAgent{}
	rest := a2asrv.NewRESTHandler(a2asrv.NewHandler(agent.executor()))
	var cardServer *httptest.Server
	cardServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agent.bearer = append(agent.bearer, r.Header.Get("Authorization"))
		if r.URL.Path == "/.well-known/agent-card.json" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(&a2asdk.AgentCard{
				Name: "the orchard keeper", Description: "Keeps the orchard.", Version: "1.0.0",
				SupportedInterfaces: []*a2asdk.AgentInterface{{URL: cardServer.URL + "/a2a", ProtocolBinding: a2asdk.TransportProtocolHTTPJSON, ProtocolVersion: a2asdk.Version}},
				DefaultInputModes:   []string{"text/plain"}, DefaultOutputModes: []string{"text/plain"},
				Skills: []a2asdk.AgentSkill{{ID: "keep", Name: "keep", Description: "Keeps.", Tags: []string{"orchard"}}},
			})
			return
		}
		http.StripPrefix("/a2a", rest).ServeHTTP(w, r)
	}))
	t.Cleanup(cardServer.Close)
	srv, tokens, _ := nodeAgentOverA2A(t, "")

	code, asked := a2aAskedOver(t, srv, tokens[auth.LevelRoot], map[string]string{"card": cardServer.URL + "/.well-known/agent-card.json", "says": "how is the harvest?"})
	require.Equal(t, http.StatusOK, code, asked)
	assert.Equal(t, "heard: how is the harvest?", asked["answer"])
	assert.Equal(t, "the orchard keeper", asked["agent"])
	for _, bearer := range agent.bearer {
		assert.Empty(t, bearer, "an agent a card names was given a token")
	}
}

// An agent is named by a peer or by a card, one of them; and a card is read
// from what the internet routes to alone (§13.2).
func TestAnAgentIsNamedOnce(t *testing.T) {
	srv, tokens, _ := nodeAgentOverA2A(t, "")
	root := tokens[auth.LevelRoot]
	for _, asked := range []map[string]string{
		{"says": "hi"},
		{"says": "hi", "peer": "orchardnode", "card": "https://example.org/card.json"},
		{"says": "hi", "card": "https://example.org/card.json", "tenant": "orchard"},
	} {
		code, refused := a2aAskedOver(t, srv, root, asked)
		assert.Equal(t, http.StatusBadRequest, code, refused)
	}
	code, refused := a2aAskedOver(t, srv, root, map[string]string{"says": "hi", "card": "http://127.0.0.1:1/card.json"})
	assert.NotEqual(t, http.StatusOK, code)
	said, _ := json.Marshal(refused)
	assert.Contains(t, string(said), "no public address")
}
