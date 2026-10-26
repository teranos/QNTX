package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// anthropicStandIn answers as Anthropic's API does: it trades an identity
// token the node's published key verifies for an access token, and holds
// one agent's environment, agent, vault and session.
type anthropicStandIn struct {
	mu       sync.Mutex
	asserted []jwt.Claims
	bodies   map[string][]string
	apiKeys  []string
	said     chan struct{}
}

func standInAnthropic(t *testing.T, s *QNTXServer) (*anthropicStandIn, string) {
	t.Helper()
	keys := httptest.NewRecorder()
	s.HandleOIDCKeys(keys, httptest.NewRequest(http.MethodGet, oidcKeysPath, nil))
	var published jose.JSONWebKeySet
	require.NoError(t, json.Unmarshal(keys.Body.Bytes(), &published))
	a := &anthropicStandIn{bodies: map[string][]string{}, said: make(chan struct{}, 1)}
	answer := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}
	served := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		a.mu.Lock()
		a.bodies[r.Method+" "+r.URL.Path] = append(a.bodies[r.Method+" "+r.URL.Path], string(body))
		if key := r.Header.Get("X-Api-Key"); key != "" {
			a.apiKeys = append(a.apiKeys, key)
		}
		a.mu.Unlock()
		if r.URL.Path == "/v1/oauth/token" {
			var exchange struct {
				Assertion string `json:"assertion"`
			}
			require.NoError(t, json.Unmarshal(body, &exchange))
			parsed, err := jwt.ParseSigned(exchange.Assertion, []jose.SignatureAlgorithm{jose.ES256})
			require.NoError(t, err)
			var claims jwt.Claims
			require.NoError(t, parsed.Claims(published.Key(parsed.Headers[0].KeyID)[0].Key, &claims), "signed with the key the node publishes")
			a.mu.Lock()
			a.asserted = append(a.asserted, claims)
			a.mu.Unlock()
			answer(w, `{"access_token":"sk-ant-oat01-traded","token_type":"Bearer","expires_in":3600}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer sk-ant-oat01-traded" {
			w.WriteHeader(http.StatusUnauthorized)
			answer(w, `{"type":"error","error":{"type":"authentication_error","message":"Authentication failed"}}`)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/environments":
			answer(w, `{"id":"env_garden","type":"environment"}`)
		case "POST /v1/agents":
			answer(w, `{"id":"agent_garden","version":1,"type":"agent"}`)
		case "POST /v1/vaults":
			answer(w, `{"id":"vlt_garden","type":"vault"}`)
		case "POST /v1/vaults/vlt_garden/credentials":
			answer(w, `{"id":"cred_qntx","type":"vault_credential"}`)
		case "POST /v1/sessions":
			answer(w, `{"id":"sesn_garden","status":"idle","type":"session"}`)
		case "POST /v1/sessions/sesn_garden/events":
			a.said <- struct{}{}
			answer(w, `{"data":[]}`)
		case "GET /v1/sessions/sesn_garden/events/stream":
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.(http.Flusher).Flush()
			<-a.said
			for _, event := range []string{
				`{"type":"agent.mcp_tool_use","id":"sevt_1","name":"am","mcp_server_name":"qntx","input":{},"processed_at":"2026-10-26T22:00:00Z"}`,
				`{"type":"agent.message","id":"sevt_2","content":[{"type":"text","text":"Up 3 days."}],"processed_at":"2026-10-26T22:00:01Z"}`,
				`{"type":"session.status_idle","id":"sevt_3","stop_reason":{"type":"end_turn"},"processed_at":"2026-10-26T22:00:02Z"}`,
			} {
				var named struct{ Type string }
				require.NoError(t, json.Unmarshal([]byte(event), &named))
				_, _ = io.WriteString(w, "event: "+named.Type+"\ndata: "+event+"\n\n")
				w.(http.Flusher).Flush()
			}
		default:
			w.WriteHeader(http.StatusNotFound)
			answer(w, `{"type":"error","error":{"type":"not_found_error","message":"`+r.URL.Path+`"}}`)
		}
	}))
	t.Cleanup(served.Close)
	return a, served.URL
}

var managedGarden = sigil.Sent{
	"namespace": "garden", "harness": "managed", "model": "claude-opus-5-5",
	"organization_id": "00000000-0000-0000-0000-000000000000", "federation_rule_id": "fdrl_garden", "service_account_id": "svac_garden",
}

// "so we can have an agent, that isnt hosted on the same box as QNTX"
// "we will not be doing API key anymore"
func TestTheManagedAgentOfANamespaceRunsAtAnthropicOnTheNodesOwnIdentity(t *testing.T) {
	s, _, in := runningNamespaceAgents(t)
	anthropicAt, base := standInAnthropic(t, s)
	s.anthropicBaseURL = base

	set, refused := rootAsksAm(s, "set", managedGarden)
	require.Nil(t, refused)
	holds(t, s.agentsSignum(), "set", set)
	managed := set.GetManaged()
	assert.Equal(t, "agent_garden", managed.GetAgentId())
	assert.Equal(t, "env_garden", managed.GetEnvironmentId())
	assert.Equal(t, "vlt_garden", managed.GetVaultId())
	agent, held := s.namespaceAgents.of(agentPurpose("garden", "claude-opus-5-5"))
	require.True(t, held)
	assert.Equal(t, agent.did, managed.GetDid())

	// The vault holds the agent's own token, for the node's MCP at its public origin.
	credentials := anthropicAt.bodies["POST /v1/vaults/vlt_garden/credentials"]
	require.Len(t, credentials, 1)
	assert.Contains(t, credentials[0], `"token":"`+agent.token+`"`)
	assert.Contains(t, credentials[0], `"mcp_server_url":"`+s.authHandler.PublicOrigin()+`/mcp"`)
	assert.NotContains(t, credentials[0], s.nodeAgent.token, "ROOT's token is not the namespace agent's")

	said, refused := answeredAs(s, visitor, auth.LevelUser, []string{"garden"}, "say", sigil.Sent{"namespace": "garden", "harness": "managed", "says": "how long has the box been up?"})
	require.Nil(t, refused)
	saidIn, isSaid := said.(*protocol.NamespaceAgentSaid)
	require.True(t, isSaid)
	holds(t, s.agentsSignum(), "say", saidIn)
	assert.Equal(t, "Up 3 days.", saidIn.GetManaged().GetAnswer())
	assert.Equal(t, "sesn_garden", saidIn.GetManaged().GetManagedSession())
	assert.Equal(t, "end_turn", saidIn.GetManaged().GetStop())

	// Every call on a token traded for the node's identity token for the agent; no key.
	assert.Empty(t, anthropicAt.apiKeys)
	require.NotEmpty(t, anthropicAt.asserted)
	for _, claims := range anthropicAt.asserted {
		assert.Equal(t, s.authHandler.PublicOrigin(), claims.Issuer)
		assert.Equal(t, agent.did, claims.Subject)
	}

	var predicates []string
	for _, row := range in.store["garden"].rows {
		if row.SignerDID == agent.did {
			predicates = append(predicates, row.Predicates[0])
		}
	}
	assert.Equal(t, []string{"UserPromptSubmit", "SessionStart", "PreToolUse", "Stop"}, predicates, "told, session start, tool, answer, signed as itself in garden")

	am, refused := rootAsksAm(s, "am", sigil.Sent{"namespace": "garden", "harness": "managed"})
	require.Nil(t, refused)
	holds(t, s.agentsSignum(), "am", am)
	assert.Equal(t, "sesn_garden", am.GetManaged().GetManagedSession())
	assert.NotEmpty(t, am.GetManaged().GetSession())

	read, refused := answeredAs(s, visitor, auth.LevelUser, []string{"garden"}, "session", sigil.Sent{"namespace": "garden", "harness": "managed"})
	require.Nil(t, refused)
	whole, isTranscript := read.(*protocol.SessionTranscript)
	require.True(t, isTranscript)
	require.NotEmpty(t, whole.GetTranscript().GetTurns())
	assert.Equal(t, "how long has the box been up?", whole.GetTranscript().GetTurns()[0].GetText())

	missing := sigil.Sent{}
	for k, v := range managedGarden {
		if !strings.HasSuffix(k, "_id") || k == "organization_id" {
			missing[k] = v
		}
	}
	_, refused = rootAsksAm(s, "set", missing)
	require.NotNil(t, refused)
}
