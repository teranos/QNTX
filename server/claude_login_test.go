package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// "The one who set's it up does, they provide their own Subscription or API key"

// standInSignInURL is the URL the stand-in prints, as Claude Code prints its own.
const standInSignInURL = "https://claude.com/cai/oauth/authorize?code=true&state=stand-in"

func loggingIn(s *QNTXServer, sent sigil.Sent) (map[string]any, *protocol.Refusal) {
	asked := httptest.NewRequest(http.MethodPost, "/api/claude/login", nil)
	h := s.claudeHarness()
	answer, refused := s.claudeLogin(sigil.WithCaller(context.Background(), asked), h, sent)
	if refused != nil {
		return nil, refused
	}
	return answer.(map[string]any), nil
}

// Signing in is Claude Code's own flow, carried by the node: login gives the
// URL Claude Code printed, the code shown there is handed back, and what the
// sign-in leaves is in the agent's own config dir. The node holds nothing.
func TestSigningInIsClaudeCodesOwnFlow(t *testing.T) {
	s, ran := runningTheRootAgent(t, opusLow)

	begun, refused := loggingIn(s, sigil.Sent{})
	require.Nil(t, refused)
	assert.Equal(t, standInSignInURL, begun["url"])
	assert.Equal(t, false, begun["signed_in"])
	holds(t, s.harnessSignum(s.claudeHarness()), "login", begun)

	args := ranWith(t, ran, "0", "args")
	assert.Equal(t, []string{"auth", "login", "--claudeai"}, args)
	assert.Contains(t, ranWith(t, ran, "0", "env"), "CLAUDE_CONFIG_DIR="+filepath.Join(s.rootAgent.home, "claude"))

	finished, refused := loggingIn(s, sigil.Sent{"code": "the-code-shown"})
	require.Nil(t, refused)
	assert.Equal(t, "", finished["url"])
	assert.Equal(t, true, finished["signed_in"])
	assert.Equal(t, "claude.ai", finished["auth_method"])
	holds(t, s.harnessSignum(s.claudeHarness()), "login", finished)

	kept, err := os.ReadFile(filepath.Join(s.rootAgent.home, "claude", ".credentials.json"))
	require.NoError(t, err)
	assert.Equal(t, "the-code-shown\n", string(kept), "what the sign-in leaves is Claude Code's, in the agent's config dir")

	is, refused := s.harnessAm(s.claudeHarness())
	require.Nil(t, refused)
	am := is.(*protocol.ClaudeAm)
	assert.True(t, am.GetSignedIn())
	assert.Equal(t, "claude.ai", am.GetAuthMethod())
	holds(t, s.harnessSignum(s.claudeHarness()), "am", am)
}

// The Console pays when asked; the subscription otherwise.
func TestWhoseAccountPaysIsNamed(t *testing.T) {
	s, ran := runningTheRootAgent(t, opusLow)
	_, refused := loggingIn(s, sigil.Sent{"billing": "console"})
	require.Nil(t, refused)
	assert.Equal(t, []string{"auth", "login", "--console"}, ranWith(t, ran, "0", "args"))
}

// A code with no sign-in begun has nothing to finish, and says what to do.
func TestACodeWithNoSignInBegunIsRefused(t *testing.T) {
	s, _ := runningTheRootAgent(t, opusLow)
	_, refused := loggingIn(s, sigil.Sent{"code": "the-code-shown"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.NotFound, refused.GetWhy())
	assert.Contains(t, refused.GetSays(), "login without a code")
}

// A code Claude Code refuses is refused in its words, and nothing is signed in.
func TestACodeClaudeCodeRefusesIsSaid(t *testing.T) {
	s, _ := runningTheRootAgent(t, opusLow)
	_, refused := loggingIn(s, sigil.Sent{})
	require.Nil(t, refused)
	_, refused = loggingIn(s, sigil.Sent{"code": "bad-code"})
	require.NotNil(t, refused)
	assert.Equal(t, sigil.Failed, refused.GetWhy())
	assert.Contains(t, refused.GetSays(), "Invalid code")
	_, err := os.Stat(filepath.Join(s.rootAgent.home, "claude", ".credentials.json"))
	assert.True(t, os.IsNotExist(err))
}

// A sign-in begun over one not finished ends the first: one URL is live.
func TestASecondSignInEndsTheFirst(t *testing.T) {
	s, _ := runningTheRootAgent(t, opusLow)
	_, refused := loggingIn(s, sigil.Sent{})
	require.Nil(t, refused)
	first := s.rootAgent.signingIn.pending
	_, refused = loggingIn(s, sigil.Sent{})
	require.Nil(t, refused)
	assert.NotSame(t, first, s.rootAgent.signingIn.pending)
	assert.Error(t, first.Finish("the-code-shown"), "the first sign-in is ended")
}

// A node whose am.toml names no plan token runs Claude Code on its own
// sign-in, so a turn before it is signed in is not run, and says what to do.
func TestATurnWithNoCredentialIsToldToSignIn(t *testing.T) {
	s, ran := runningTheRootAgent(t, opusLow)
	s.deps.cfg.Agent.Root.TokenRef = ""

	_, refused := saying(s, sigil.Sent{"says": "hello"})
	require.NotNil(t, refused)
	assert.Contains(t, refused.GetSays(), "claude login")

	_, refused = loggingIn(s, sigil.Sent{})
	require.Nil(t, refused)
	_, refused = loggingIn(s, sigil.Sent{"code": "the-code-shown"})
	require.Nil(t, refused)

	answer, refused := saying(s, sigil.Sent{"says": "hello"})
	require.Nil(t, refused)
	assert.Equal(t, "Up 3 days.", answer.GetAnswer())
	// The turn carries no plan token: Claude Code runs on what the sign-in left.
	for _, run := range []string{"0", "1", "2", "3", "4"} {
		if env, err := os.ReadFile(filepath.Join(ran, run, "env")); err == nil {
			assert.NotContains(t, string(env), "CLAUDE_CODE_OAUTH_TOKEN=")
		}
	}
}
