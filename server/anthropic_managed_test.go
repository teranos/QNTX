package server

import (
	"encoding/base64"
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
	"time"

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
	// spent is what the Session has cost, by each time it is read.
	spent float64
	// budget is each Session's budget as it was made or last updated; absent is none.
	budget map[string]any
	// stop is the stop_reason the Session goes idle with: end_turn when empty.
	stop string
	// running is each Session still at work until it is interrupted; archived, each archived.
	running, archived map[string]bool
}

func standInConsole(t *testing.T) (*consoleStandIn, string) {
	t.Helper()
	c := &consoleStandIn{made: map[string]int{}, bodies: map[string][]map[string]any{}, said: make(chan string, 4), answers: "the element answered at Anthropic", ended: map[string]bool{}, budget: map[string]any{}, running: map[string]bool{}, archived: map[string]bool{}}
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
		case r.Method == http.MethodGet && r.URL.Path == "/v1/agents/agent_1":
			last := c.bodies["agent"][len(c.bodies["agent"])-1]
			agent, _ := json.Marshal(map[string]any{"id": "agent_1", "version": len(c.bodies["agent"]), "system": last["system"], "model": last["model"]})
			_, _ = w.Write(agent)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/agents/agent_1":
			c.made["agent version"]++
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
			id := fmt.Sprintf("sesn_%d", c.made["session"])
			if budget, capped := sent["budget"]; capped {
				c.budget[id] = budget
			}
			answer(id)
		case r.Method == http.MethodGet && strings.Count(r.URL.Path, "/") == 3 && strings.HasPrefix(r.URL.Path, "/v1/sessions/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/sessions/")
			status := "idle"
			if c.running[id] {
				status = "running"
			}
			if c.ended[id] || c.archived[id] {
				status = "terminated"
			}
			c.spent += 0.25
			budget, _ := json.Marshal(c.budget[id])
			_, _ = io.WriteString(w, fmt.Sprintf(`{"id":"%s","status":"%s","budget":%s,"usage":{"list_cost":{"amount":"%.2f","currency":"USD"}}}`, id, status, budget, c.spent))
		case r.Method == http.MethodPost && strings.Count(r.URL.Path, "/") == 3 && strings.HasPrefix(r.URL.Path, "/v1/sessions/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/sessions/")
			c.made["session update"]++
			c.bodies["session update"] = append(c.bodies["session update"], sent)
			c.budget[id] = sent["budget"]
			answer(id)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/archive"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/sessions/"), "/archive")
			if c.running[id] {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"type":"error","error":{"type":"invalid_request_error","message":"a running session cannot be archived"}}`)
				break
			}
			c.made["archive"]++
			c.archived[id] = true
			answer(id)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/events"):
			events, _ := sent["events"].([]any)
			first, _ := events[0].(map[string]any)
			if first["type"] == "user.interrupt" {
				c.made["interrupt"]++
				delete(c.running, strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/sessions/"), "/events"))
				_, _ = io.WriteString(w, `{"data":[]}`)
				break
			}
			content, _ := first["content"].([]any)
			c.bodies["said"] = append(c.bodies["said"], map[string]any{"content": content})
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
			stop := c.stop
			if stop == "" {
				stop = "end_turn"
			}
			for _, event := range [][2]string{
				{"agent.mcp_tool_use", `{"id":"ev_0","type":"agent.mcp_tool_use","processed_at":"2026-10-27T18:00:00Z","mcp_server_name":"qntx","name":"am_version","input":{}}`},
				{"agent.message", `{"id":"ev_1","type":"agent.message","processed_at":"2026-10-27T18:00:00Z","content":[{"type":"text","text":"` + c.answers + `"}]}`},
				{"session.status_idle", `{"id":"ev_2","type":"session.status_idle","processed_at":"2026-10-27T18:00:01Z","stop_reason":{"type":"` + stop + `"},"stop_details":{}}`},
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
	answer, err := s.spend(agent, said, shaken{}, func(m claudecode.Message) { seen = append(seen, m) })
	require.NoError(t, err)
	assert.Equal(t, "the element answered at Anthropic", answer.Text)
	assert.False(t, answer.IsError)
	assert.Equal(t, "sesn_1", answer.Session)
	assert.InDelta(t, 0.25, answer.CostUSD, 0.001, "what the Session cost, read when the turn ended")
	// What it reached for is shown as it happens, then the answer.
	require.Len(t, seen, 2)
	assert.Equal(t, "assistant", seen[0].Type)
	assert.Equal(t, "mcp__qntx__am_version", seen[0].Blocks()[0].Name)
	assert.Equal(t, "the element answered at Anthropic", seen[1].Result)

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

	again, err := s.spend(agent, said, shaken{}, func(claudecode.Message) {})
	require.NoError(t, err)
	assert.Equal(t, "sesn_1", again.Session, "shaken again: the same Session")
	console.mu.Lock()
	assert.Equal(t, map[string]int{"environment": 1, "agent": 1, "vault": 1, "credential": 1, "memory": 1, "session": 1}, console.made, "each made once")
	console.mu.Unlock()

	// A screenshot reaches it as a picture, not as a path on the node it cannot open.
	picture := filepath.Join(t.TempDir(), "shot.png")
	png := []byte("\x89PNG\r\n\x1a\n" + "the rest of a picture")
	require.NoError(t, os.WriteFile(picture, png, 0o600))
	_, err = s.spend(agent, said, shaken{says: "weird resize happens", element: "canvas-1", screenshot: picture}, func(claudecode.Message) {})
	require.NoError(t, err)
	console.mu.Lock()
	sent := console.bodies["said"][len(console.bodies["said"])-1]["content"].([]any)
	console.mu.Unlock()
	require.Len(t, sent, 2)
	assert.Equal(t, "weird resize happens\n\nThe screenshot of element canvas-1 is with this message.", sent[0].(map[string]any)["text"])
	source := sent[1].(map[string]any)["source"].(map[string]any)
	assert.Equal(t, "image/png", source["media_type"])
	assert.Equal(t, base64.StdEncoding.EncodeToString(png), source["data"])
	assert.NotContains(t, sent[0].(map[string]any)["text"], picture, "no path on the node")

	kept, err := os.ReadFile(filepath.Join(home, elementsDir, "canvas-1", managedElementFile))
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(kept), `"session":"sesn_1"`), string(kept))

	// A Session Anthropic ended is said, and the element goes on in a new one, on the same Agent and Vault.
	console.mu.Lock()
	console.ended["sesn_1"] = true
	console.mu.Unlock()
	anew, err := s.spend(agent, said, shaken{}, func(claudecode.Message) {})
	require.NoError(t, err)
	assert.Equal(t, "sesn_2", anew.Session)
	console.mu.Lock()
	assert.Equal(t, map[string]int{"environment": 1, "agent": 1, "vault": 1, "credential": 1, "memory": 1, "session": 2}, console.made)
	assert.Equal(t, console.bodies["session"][0]["resources"], console.bodies["session"][1]["resources"], "the new Session has the Memory the ended one had")
	console.mu.Unlock()

	// Another model: its Agent becomes a new version, and it goes on in a new Session with the same Memory.
	console.mu.Lock()
	sessionsBefore := console.made["session"]
	console.mu.Unlock()
	onSonnet := said
	onSonnet.Model = "claude-sonnet-5-5"
	moved, err := s.spend(agent, onSonnet, shaken{}, func(claudecode.Message) {})
	require.NoError(t, err)
	console.mu.Lock()
	assert.Equal(t, 1, console.made["agent version"])
	updated := console.bodies["agent"][len(console.bodies["agent"])-1]
	assert.Equal(t, "claude-sonnet-5-5", updated["model"].(map[string]any)["id"])
	assert.Equal(t, sessionsBefore+1, console.made["session"])
	console.mu.Unlock()
	assert.NotEqual(t, "sesn_1", moved.Session)

}

// "Am Agent should have a direct link to A2A Agent and reverse"
func TestAnElementsAgentKnowsTheOthersAndWhichItWorksWithDirectly(t *testing.T) {
	am := managedIsSaidToBe(&rootAgent{element: "am-element", did: "did:key:am"})
	assert.Contains(t, am, "Every element on the page is an agent of its own")
	assert.Contains(t, am, "You work directly with a2a-card.")
	assert.Contains(t, managedIsSaidToBe(&rootAgent{element: "a2a-card", did: "did:key:a2a"}), "You work directly with am-element.")
	assert.NotContains(t, managedIsSaidToBe(&rootAgent{element: "ax-1", did: "did:key:ax"}), "You work directly with")
}

// "having one agent be always in tune with one story, it would make it so you never have to make it shift frame, the contract is always clear."
func TestAStorysAgentHasItsOneStoryAsItsContract(t *testing.T) {
	story := managedIsSaidToBe(&rootAgent{element: "story-27-root-registers-the-node-with-anthropic", did: "did:key:story"})
	assert.Contains(t, story, "You are the user story stories/27-root-registers-the-node-with-anthropic.feature of github.com/teranos/QNTX-Web, and nothing else")
	assert.NotContains(t, story, "You are the element")
	// "and because some stories may lead into each other, the stories could talk to the stories as well"
	assert.Contains(t, story, "So is every other story, named story- and its file's name")
	element := managedIsSaidToBe(&rootAgent{element: "approvals-element", did: "did:key:approvals"})
	assert.Contains(t, element, "The user stories in stories/ of github.com/teranos/QNTX-Web that name you are your contract")
}

// "make 6SION set one, prep a pr"
// "what  is max cap? like set it in the ui and make default of 200"
// "6SION00: The bad session needs to not be able to murder us further."
// 「[Opus 5.5] Each Session an element's agent runs in at Anthropic is made with a cap: the node's」
// 「[Opus 5.5] default until ROOT sets one in Node Connectors (platform.claude.com/docs/en/managed-agents/budgets).」
func TestAnElementsSessionAtAnthropicIsMadeWithTheCapRootSet(t *testing.T) {
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
	agent := &rootAgent{home: home, namespace: auth.NamespaceSystem, element: "llm-provider-element", sessionDir: filepath.Join(elementsDir, "llm-provider-element"), did: "did:key:element", token: "the-element-token"}
	said := rootSaid(filepath.Join(t.TempDir(), "no-claude-code-is-run"), home)
	budget := func(cents string) any {
		return map[string]any{"type": "limit", "max_list_cost": map[string]any{"amount": cents, "currency": anthropicCapCurrency}}
	}

	// None set: the Session is made with the node's default, in the cents Anthropic takes.
	answer, err := s.spend(agent, said, shaken{}, func(claudecode.Message) {})
	require.NoError(t, err)
	assert.Equal(t, "sesn_1", answer.Session)
	console.mu.Lock()
	assert.Equal(t, budget("20000"), console.bodies["session"][0]["budget"], "the default, 200")
	console.mu.Unlock()

	// ROOT sets another: the element goes on in a new Session made with it, and the old one is archived.
	// Qodo's review of #1519: updated in place, the old cap's paused work resumed before this turn's
	// stream was open, and a cap below what was spent was refused on every turn after.
	rootAsksAnthropic(t, s, "cap", sigil.Sent{"amount": "30"})
	again, err := s.spend(agent, said, shaken{}, func(claudecode.Message) {})
	require.NoError(t, err)
	assert.Equal(t, "sesn_2", again.Session)
	console.mu.Lock()
	assert.Zero(t, console.made["session update"], "no Session's cap is changed in place")
	assert.True(t, console.archived["sesn_1"])
	assert.Equal(t, budget("3000"), console.bodies["session"][1]["budget"])
	assert.Equal(t, console.bodies["session"][0]["resources"], console.bodies["session"][1]["resources"], "with the same Memory")
	console.mu.Unlock()

	// A Session kept from before, made without a cap, cannot be given one: it goes on in a new one that has it.
	// Qodo's review of #1519: the uncapped one is ended at Anthropic first, interrupted while it
	// still runs, so it spends nothing beside the capped one.
	retireAsks = time.Millisecond
	t.Cleanup(func() { retireAsks = time.Second })
	console.mu.Lock()
	delete(console.budget, "sesn_2")
	console.running["sesn_2"] = true
	console.mu.Unlock()
	anew, err := s.spend(agent, said, shaken{}, func(claudecode.Message) {})
	require.NoError(t, err)
	assert.Equal(t, "sesn_3", anew.Session)
	console.mu.Lock()
	assert.Equal(t, 1, console.made["interrupt"], "the uncapped Session was running, and is interrupted")
	assert.True(t, console.archived["sesn_2"], "and archived before another is made")
	assert.Equal(t, budget("3000"), console.bodies["session"][2]["budget"])
	assert.Equal(t, console.bodies["session"][0]["resources"], console.bodies["session"][2]["resources"], "with the Memory the uncapped one had")
	console.mu.Unlock()

	// At its cap the Session pauses: the turn is an error that says so, and names the cap.
	console.mu.Lock()
	console.stop = "budget_reached"
	console.mu.Unlock()
	var ended []claudecode.Message
	capped, err := s.spend(agent, said, shaken{}, func(m claudecode.Message) { ended = append(ended, m) })
	require.NoError(t, err)
	assert.True(t, capped.IsError)
	// Qodo's review of #1519: the result said success, so live showed a failed turn as "success: …".
	assert.Equal(t, "budget_reached", capped.Subtype)
	require.NotEmpty(t, ended)
	assert.Equal(t, "budget_reached", ended[len(ended)-1].Subtype)
	assert.Contains(t, capped.Text, "the element answered at Anthropic", "what it said before the cap is kept")
	assert.Contains(t, capped.Text, "reached its cap of 30 "+anthropicCapCurrency)
	assert.Contains(t, capped.Text, "Node Connectors")
}
