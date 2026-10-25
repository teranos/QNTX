package server

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/signing"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var codexGarden = sigil.Sent{"namespace": "garden", "harness": "codex", "model": "test-codex", "sandbox": "read-only"}

func TestCodexNamespaceConfiguration(t *testing.T) {
	s, in := codexNamespaceNode(t)
	am, refused := askingAm(s, gardener, auth.LevelToken, []string{"garden"}, "set", codexGarden)
	require.Nil(t, refused)
	require.Equal(t, "test-codex", am.Codex.Model)
	require.Equal(t, "read-only", am.Codex.Sandbox)
	require.NotEmpty(t, am.Codex.NotReady)
	require.Equal(t, []string{"codex"}, in.store["garden"].rows[0].Predicates)
	holds(t, s.agentsSignum(), "set", am)
	_, refused = askingAm(s, visitor, auth.LevelToken, []string{"orchard"}, "am", codexGarden)
	require.NotNil(t, refused)
	require.Equal(t, sigil.NotFound, refused.Why)
	_, refused = askingAm(s, visitor, auth.LevelToken, []string{"garden"}, "set", codexGarden)
	require.NotNil(t, refused)
	require.Equal(t, sigil.NotAllowed, refused.Why)
}

func TestCodexNamespaceRequiresCredentialsBeforeTurn(t *testing.T) {
	s, in := codexNamespaceNode(t)
	_, refused := rootAsksAm(s, "set", codexGarden)
	require.Nil(t, refused)
	_, refused = answeredAs(s, gardener, auth.LevelToken, []string{"garden"}, "say", sigil.Sent{"namespace": "garden", "harness": "codex", "says": "hello"})
	require.NotNil(t, refused)
	require.Contains(t, refused.Says, "credential")
	require.Len(t, in.store["garden"].rows, 1)
}

func TestCodexNamespaceTurnResumesAndSigns(t *testing.T) {
	s, in := codexNamespaceNode(t)
	_, refused := rootAsksAm(s, "set", codexGarden)
	require.Nil(t, refused)
	agent, ok := s.namespaceAgents.of(agentPurpose("garden", "test-codex"))
	require.True(t, ok)
	require.NoError(t, os.MkdirAll(filepath.Join(agent.home, "codex"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(agent.home, "codex", "auth.json"), []byte(`{"test":"fixture only"}`), 0600))
	binary := filepath.Join(t.TempDir(), "codex")
	require.NoError(t, os.WriteFile(binary, []byte(`#!/bin/sh
cat >/dev/null
printf '%s\n' "$@" >> calls
printf '%s\n' '{"type":"thread.started","thread_id":"codex-thread"}' '{"type":"item.completed","item":{"id":"m","type":"agent_message","text":"hello garden"}}' '{"type":"turn.completed","usage":{"input_tokens":2,"output_tokens":3}}'
`), 0700))
	arrived := make(chan struct{})
	close(arrived)
	s.holdHarness("codex", &harnessHeld{name: "Codex", path: binary, fetched: arrived})
	// Codex must not need the Claude executable or the Node Agent.
	s.harnessBinaries.mu.Lock()
	delete(s.harnessBinaries.held, "claude")
	s.harnessBinaries.mu.Unlock()
	s.nodeAgent = nil
	var session string
	for range 2 {
		answer, refused := answeredAs(s, gardener, auth.LevelToken, []string{"garden"}, "say", sigil.Sent{"namespace": "garden", "harness": "codex", "says": "hello"})
		require.Nil(t, refused)
		said := answer.(*protocol.NamespaceAgentSaid).Codex
		require.Equal(t, "hello garden", said.Answer)
		require.Equal(t, "codex-thread", said.Thread)
		require.EqualValues(t, 2, said.InputTokens)
		require.EqualValues(t, 3, said.OutputTokens)
		if session != "" {
			require.Equal(t, session, said.Session)
		}
		session = said.Session
		holds(t, s.agentsSignum(), "say", answer)
	}
	require.NotEqual(t, "codex-thread", session)
	calls, err := os.ReadFile(filepath.Join(agent.home, "work", "calls"))
	require.NoError(t, err)
	require.Contains(t, string(calls), "resume\n--\ncodex-thread\n-\n")
	for _, row := range in.store["garden"].rows[1:] {
		require.Equal(t, []string{agent.did}, row.Actors)
		require.Equal(t, []string{"session:" + session}, row.Contexts)
		require.NotEmpty(t, row.Signature)
		require.NoError(t, signing.Verify(row))
	}
	transcript, refused := answeredAs(s, gardener, auth.LevelToken, []string{"garden"}, "session", codexGarden)
	require.Nil(t, refused)
	require.NotNil(t, transcript.(*protocol.SessionTranscript).Transcript)
}

func codexNamespaceNode(t *testing.T) (*QNTXServer, gardens) {
	s, _, in := runningNamespaceAgents(t)
	home, err := s.agentHome("garden", "test-codex")
	require.NoError(t, err)
	underGround(t, home)
	s.underGround.add(home)
	return s, in
}

func TestCodexNamespaceKeepsFailedThreadAndResumesAfterReload(t *testing.T) {
	s, in := codexNamespaceNode(t)
	_, refused := rootAsksAm(s, "set", codexGarden)
	require.Nil(t, refused)
	agent, ok := s.namespaceAgents.of(agentPurpose("garden", "test-codex"))
	require.True(t, ok)
	require.NoError(t, os.MkdirAll(filepath.Join(agent.home, "codex"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(agent.home, "codex", "auth.json"), []byte(`{"fixture":true}`), 0600))
	binary := filepath.Join(t.TempDir(), "codex")
	require.NoError(t, os.WriteFile(binary, []byte(`#!/bin/sh
cat >/dev/null
printf '%s\n' '{"type":"thread.started","thread_id":"failed-thread"}' '{"type":"turn.failed","error":{"message":"provider unavailable"}}'
exit 1
`), 0700))
	arrived := make(chan struct{})
	close(arrived)
	s.holdHarness("codex", &harnessHeld{name: "Codex", path: binary, fetched: arrived})
	_, refused = answeredAs(s, gardener, auth.LevelToken, []string{"garden"}, "say", sigil.Sent{"namespace": "garden", "harness": "codex", "says": "first"})
	require.NotNil(t, refused)
	session, kept, err := agent.sessionIn(codexSessionFile)
	require.NoError(t, err)
	require.True(t, kept)
	thread, err := os.ReadFile(filepath.Join(agent.home, codexThreadFile))
	require.NoError(t, err)
	require.Equal(t, "failed-thread\n", string(thread))
	require.Equal(t, []string{"StopFailure"}, in.store["garden"].rows[len(in.store["garden"].rows)-1].Predicates)
	require.NoError(t, os.WriteFile(binary, []byte(`#!/bin/sh
cat >/dev/null
printf '%s\n' "$@" > resumed-args
printf '%s\n' '{"type":"thread.started","thread_id":"failed-thread"}' '{"type":"item.completed","item":{"type":"agent_message","text":"recovered"}}' '{"type":"turn.completed","usage":{}}'
`), 0700))
	// A new in-memory agent must read both identifiers from disk.
	s.namespaceAgents = agentsHeld{}
	answer, refused := answeredAs(s, gardener, auth.LevelToken, []string{"garden"}, "say", sigil.Sent{"namespace": "garden", "harness": "codex", "says": "again"})
	require.Nil(t, refused)
	require.Equal(t, session, answer.(*protocol.NamespaceAgentSaid).Codex.Session)
	args, err := os.ReadFile(filepath.Join(agent.home, "work", "resumed-args"))
	require.NoError(t, err)
	require.Contains(t, string(args), "resume\n--\nfailed-thread\n")
}

func TestCodexNamespaceRefusesWithoutGround(t *testing.T) {
	s, _ := codexNamespaceNode(t)
	_, refused := rootAsksAm(s, "set", codexGarden)
	require.Nil(t, refused)
	agent, _ := s.namespaceAgents.of(agentPurpose("garden", "test-codex"))
	require.NoError(t, os.MkdirAll(filepath.Join(agent.home, "codex"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(agent.home, "codex", "auth.json"), []byte(`{"fixture":true}`), 0600))
	require.NoError(t, os.Remove(filepath.Join(agent.home, "bin", "ground")))
	_, refused = answeredAs(s, gardener, auth.LevelToken, []string{"garden"}, "say", sigil.Sent{"namespace": "garden", "harness": "codex", "says": "hello"})
	require.NotNil(t, refused)
	require.Contains(t, refused.Says, "ground")
}

func TestCodexNamespaceRequiresExplicitSandbox(t *testing.T) {
	for _, sandbox := range []string{"", "danger-full-access", "unknown"} {
		t.Run(sandbox, func(t *testing.T) {
			s, in := codexNamespaceNode(t)
			sent := sigil.Sent{"namespace": "garden", "harness": "codex", "model": "test-codex", "sandbox": sandbox}
			_, refused := rootAsksAm(s, "set", sent)
			require.NotNil(t, refused)
			require.Equal(t, "sandbox", refused.Param)
			require.Empty(t, in.store["garden"].rows)
		})
	}
}

func TestCodexNamespaceSharesTheAgentTurnLock(t *testing.T) {
	s, _ := codexNamespaceNode(t)
	_, refused := rootAsksAm(s, "set", codexGarden)
	require.Nil(t, refused)
	agent, _ := s.namespaceAgents.of(agentPurpose("garden", "test-codex"))
	agent.in(s.claudeHarness()).turn <- struct{}{}
	defer func() { <-agent.in(s.claudeHarness()).turn }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	asked := httptest.NewRequest(http.MethodPost, "/api/agents/garden", nil)
	admitted := auth.Admitted(auth.LevelToken, "garden")
	admitted.Identity = gardener
	asked = asked.WithContext(auth.WithAdmission(ctx, admitted))
	_, refused = s.codexSay(sigil.WithCaller(asked.Context(), asked), sigil.Sent{"namespace": "garden", "harness": "codex", "says": "hello"})
	require.NotNil(t, refused)
	require.Contains(t, refused.Says, "still answering")
}
