package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/claudecode"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// consoleStandIn answers as Anthropic's Managed Agents do, on a token traded for
// the node's identity: each object made is counted, and a session's stream
// answers what was sent to it, then goes idle.
type consoleStandIn struct {
	mu      sync.Mutex
	made    map[string]int
	bodies  map[string][]map[string]any
	said    chan string
	answers string
	// ended is each Session Anthropic has terminated.
	ended map[string]bool
}

func standInConsole(t *testing.T) (*consoleStandIn, string) {
	t.Helper()
	c := &consoleStandIn{made: map[string]int{}, bodies: map[string][]map[string]any{}, said: make(chan string, 4), answers: "the element answered at Anthropic", ended: map[string]bool{}}
	served := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var sent map[string]any
		_ = json.Unmarshal(body, &sent)
		c.mu.Lock()
		defer c.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		answer := func(id string) { _, _ = io.WriteString(w, `{"id":"`+id+`"}`) }
		switch {
		case r.URL.Path == "/v1/oauth/token":
			_, _ = io.WriteString(w, `{"access_token":"sk-ant-oat01-traded","token_type":"Bearer","expires_in":3600}`)
		case r.Header.Get("Authorization") != "Bearer sk-ant-oat01-traded":
			w.WriteHeader(http.StatusUnauthorized)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/environments":
			c.made["environment"]++
			answer("env_1")
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agents":
			c.made["agent"]++
			c.bodies["agent"] = append(c.bodies["agent"], sent)
			answer("agent_1")
		case r.Method == http.MethodPost && r.URL.Path == "/v1/vaults":
			c.made["vault"]++
			answer("vlt_1")
		case r.Method == http.MethodPost && r.URL.Path == "/v1/vaults/vlt_1/credentials":
			c.made["credential"]++
			c.bodies["credential"] = append(c.bodies["credential"], sent)
			answer("vcrd_1")
		case r.Method == http.MethodPost && r.URL.Path == "/v1/memory_stores":
			c.made["memory"]++
			answer("memstore_1")
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sessions":
			c.made["session"]++
			c.bodies["session"] = append(c.bodies["session"], sent)
			answer(fmt.Sprintf("sesn_%d", c.made["session"]))
		case r.Method == http.MethodGet && strings.Count(r.URL.Path, "/") == 3 && strings.HasPrefix(r.URL.Path, "/v1/sessions/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/sessions/")
			status := "idle"
			if c.ended[id] {
				status = "terminated"
			}
			_, _ = io.WriteString(w, `{"id":"`+id+`","status":"`+status+`"}`)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/events"):
			events, _ := sent["events"].([]any)
			first, _ := events[0].(map[string]any)
			content, _ := first["content"].([]any)
			text, _ := content[0].(map[string]any)
			said, _ := text["text"].(string)
			c.said <- said
			_, _ = io.WriteString(w, `{"data":[]}`)
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/events/stream"):
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			flusher.Flush()
			c.mu.Unlock()
			<-c.said
			c.mu.Lock()
			for _, event := range [][2]string{
				{"agent.message", `{"id":"ev_1","type":"agent.message","processed_at":"2026-10-27T18:00:00Z","content":[{"type":"text","text":"` + c.answers + `"}]}`},
				{"session.status_idle", `{"id":"ev_2","type":"session.status_idle","processed_at":"2026-10-27T18:00:01Z","stop_reason":{"type":"end_turn"},"stop_details":{}}`},
			} {
				_, _ = io.WriteString(w, "event: "+event[0]+"\ndata: "+event[1]+"\n\n")
				flusher.Flush()
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(served.Close)
	return c, served.URL
}

// "so we can have an agent, that isnt hosted on the same box as QNTX"
// "if we use their infra, we use their infra"
func TestAnElementsAgentOnTheConsoleRunsAtAnthropicAndGoesOnInItsOneSession(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	console, base := standInConsole(t)
	s.anthropicBaseURL = base
	if s.nodeAgent == nil {
		s.nodeAgent = &rootAgent{home: t.TempDir()}
	}
	for _, given := range []sigil.Sent{anOrganization, aServiceAcct, aRule} {
		rootAsksAnthropic(t, s, "set", given)
	}
	rootAsksAnthropic(t, s, "order", sigil.Sent{"first": anthropicConsole})
	home := t.TempDir()
	agent := &rootAgent{home: home, namespace: auth.NamespaceSystem, element: "canvas-1", sessionDir: filepath.Join(elementsDir, "canvas-1"), did: "did:key:element", token: "the-element-token"}
	said := rootSaid(filepath.Join(t.TempDir(), "no-claude-code-is-run"), home)

	var seen []claudecode.Message
	answer, err := s.spend(agent, said, func(m claudecode.Message) { seen = append(seen, m) })
	require.NoError(t, err)
	assert.Equal(t, "the element answered at Anthropic", answer.Text)
	assert.False(t, answer.IsError)
	assert.Equal(t, "sesn_1", answer.Session)
	require.Len(t, seen, 1)
	assert.Equal(t, "the element answered at Anthropic", seen[0].Result)

	console.mu.Lock()
	agentMade := console.bodies["agent"][0]
	assert.Equal(t, "claude-opus-5-5", agentMade["model"].(map[string]any)["id"])
	assert.Contains(t, agentMade["system"], "You run at Anthropic")
	servers := agentMade["mcp_servers"].([]any)
	assert.Equal(t, s.authHandler.PublicOrigin()+"/mcp", servers[0].(map[string]any)["url"], "it reaches the node's MCP at its public origin")
	credential := console.bodies["credential"][0]["auth"].(map[string]any)
	assert.Equal(t, map[string]any{"type": "static_bearer", "token": "the-element-token", "mcp_server_url": s.authHandler.PublicOrigin() + "/mcp"}, credential, "with its own token")
	assert.Equal(t, []any{"vlt_1"}, console.bodies["session"][0]["vault_ids"])
	assert.Equal(t, []any{map[string]any{"type": "memory_store", "memory_store_id": "memstore_1"}}, console.bodies["session"][0]["resources"], "its Memory, in every Session")
	console.mu.Unlock()

	again, err := s.spend(agent, said, func(claudecode.Message) {})
	require.NoError(t, err)
	assert.Equal(t, "sesn_1", again.Session, "shaken again: the same Session")
	console.mu.Lock()
	assert.Equal(t, map[string]int{"environment": 1, "agent": 1, "vault": 1, "credential": 1, "memory": 1, "session": 1}, console.made, "each made once")
	console.mu.Unlock()

	kept, err := os.ReadFile(filepath.Join(home, elementsDir, "canvas-1", managedElementFile))
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(kept), `"session":"sesn_1"`), string(kept))

	// A Session Anthropic ended is said, and the element goes on in a new one, on the same Agent and Vault.
	console.mu.Lock()
	console.ended["sesn_1"] = true
	console.mu.Unlock()
	anew, err := s.spend(agent, said, func(claudecode.Message) {})
	require.NoError(t, err)
	assert.Equal(t, "sesn_2", anew.Session)
	console.mu.Lock()
	assert.Equal(t, map[string]int{"environment": 1, "agent": 1, "vault": 1, "credential": 1, "memory": 1, "session": 2}, console.made)
	assert.Equal(t, console.bodies["session"][0]["resources"], console.bodies["session"][1]["resources"], "the new Session has the Memory the ended one had")
	console.mu.Unlock()
}
