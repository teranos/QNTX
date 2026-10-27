package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/claudecode"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// spending stands in for Claude Code as Anthropic answers it: on the session its
// session limit, on the node's OIDC an answer. Each run's env is kept in ran, one file a run.
func spending(t *testing.T) (binary, ran string) {
	t.Helper()
	dir := t.TempDir()
	ran = filepath.Join(dir, "ran")
	require.NoError(t, os.MkdirAll(ran, 0o755))
	limited := `{"type":"result","subtype":"success","session_id":"s-1","is_error":true,"result":"You've hit your session limit · resets 3:45pm"}`
	answered := `{"type":"result","subtype":"success","session_id":"s-1","is_error":false,"result":"answered on the Console"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "limited"), []byte(limited+"\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "answered"), []byte(answered+"\n"), 0o644))
	script := "#!/bin/sh\n" +
		"env > '" + ran + "/'$(date +%s%N)\n" +
		"if [ -n \"$ANTHROPIC_FEDERATION_RULE_ID\" ]; then cat '" + dir + "/answered'; else cat '" + dir + "/limited'; fi\n"
	binary = filepath.Join(dir, "claude")
	require.NoError(t, os.WriteFile(binary, []byte(script), 0o755))
	return binary, ran
}

func rootSaid(binary, home string) claudecode.Said {
	return claudecode.Said{Binary: binary, Home: home, Session: "s-1", Says: "what is up", Model: "claude-opus-5-5", Effort: "low",
		Mode: "dontAsk", Token: "the-plan-token", Started: func(string) {}}
}

// "we use session first until exhaustion"
// "Optionally declare that iodc Console balance will be utilised when session is exhausted"
func TestTheSessionExhaustedRootsAgentGoesOnOnTheConsoleWhenRootTickedIt(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	for _, given := range []sigil.Sent{anOrganization, aServiceAcct, aRule} {
		rootAsksAnthropic(t, s, "set", given)
	}
	binary, ran := spending(t)
	agent := &rootAgent{home: t.TempDir(), namespace: auth.NamespaceSystem}

	rootAsksAnthropic(t, s, "order", sigil.Sent{"first": anthropicSession})
	stopped, err := s.spend(agent, rootSaid(binary, agent.home), shaken{}, func(claudecode.Message) {})
	require.NoError(t, err)
	assert.True(t, stopped.IsError, "nothing follows: the turn ends exhausted")
	assert.Contains(t, stopped.Text, "session limit")
	assert.Equal(t, anthropicSession, s.anthropicInUse.Load())

	rootAsksAnthropic(t, s, "order", sigil.Sent{"first": anthropicSession, "then": "true"})
	went, err := s.spend(agent, rootSaid(binary, agent.home), shaken{}, func(claudecode.Message) {})
	require.NoError(t, err)
	assert.False(t, went.IsError, went.Text)
	assert.Equal(t, "answered on the Console", went.Text)
	assert.Equal(t, anthropicConsole, s.anthropicInUse.Load(), "the mark of what is in use moves to it")

	runs, err := os.ReadDir(ran)
	require.NoError(t, err)
	require.Len(t, runs, 3)
	console, err := os.ReadFile(filepath.Join(ran, runs[2].Name()))
	require.NoError(t, err)
	env := string(console)
	for _, want := range []string{"ANTHROPIC_FEDERATION_RULE_ID=fdrl_node", "ANTHROPIC_ORGANIZATION_ID=377885f2-bf73-4758-9338-dcfe26b37568", "ANTHROPIC_SERVICE_ACCOUNT_ID=svac_node"} {
		assert.Contains(t, env, want)
	}
	assert.NotContains(t, env, "CLAUDE_CODE_OAUTH_TOKEN", "the plan token outranks federation: on the Console it is not handed over")

	// The token Claude Code trades is the node's identity.
	file := filepath.Join(agent.home, identityTokenFile)
	assert.Contains(t, env, "ANTHROPIC_IDENTITY_TOKEN_FILE="+file)
	written, err := os.ReadFile(file)
	require.NoError(t, err)
	parsed, err := jwt.ParseSigned(strings.TrimSpace(string(written)), []jose.SignatureAlgorithm{jose.ES256})
	require.NoError(t, err)
	var claims jwt.Claims
	require.NoError(t, parsed.UnsafeClaimsWithoutVerification(&claims))
	assert.Equal(t, s.nodeDID.DID, claims.Subject)
}

// "this applies to all agents belonging to ROOT."
func TestWhatRootsAgentsSpendIsOrderedAndKeptWithTheRule(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)

	before := rootAsksAnthropic(t, s, "am", sigil.Sent{})
	assert.Equal(t, anthropicSession, before.GetFirst(), "nothing ordered: the session first")
	assert.False(t, before.GetThen())

	ordered := rootAsksAnthropic(t, s, "order", sigil.Sent{"first": anthropicConsole, "then": "true"})
	assert.Equal(t, anthropicConsole, ordered.GetFirst())
	assert.True(t, ordered.GetThen())

	given := rootAsksAnthropic(t, s, "set", aRule)
	assert.Equal(t, anthropicConsole, given.GetFirst(), "an id given keeps the order")
	assert.True(t, given.GetThen())
	assert.Equal(t, "fdrl_node", given.GetFederationRuleId())

	again := rootAsksAnthropic(t, s, "order", sigil.Sent{"first": anthropicSession})
	assert.False(t, again.GetThen(), "then not sent: nothing follows")
	assert.Equal(t, "fdrl_node", again.GetFederationRuleId(), "an order kept keeps the ids")
}

func TestAnotherAgentsTurnRunsAsSaid(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	rootAsksAnthropic(t, s, "order", sigil.Sent{"first": anthropicConsole, "then": "true"})
	binary, _ := spending(t)
	agent := &rootAgent{home: t.TempDir(), namespace: "pond"}
	answer, err := s.spend(agent, rootSaid(binary, agent.home), shaken{}, func(claudecode.Message) {})
	require.NoError(t, err)
	assert.Contains(t, answer.Text, "session limit", "a namespace's agent is not ROOT's: it spends as it was signed in")
}
