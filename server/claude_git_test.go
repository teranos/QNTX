package server

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/sigil"
)

// gitSays is what git reads of one key from the global config it is pointed at.
func gitSays(t *testing.T, config, key string) string {
	t.Helper()
	cmd := exec.Command("git", "config", "--global", "--get", key)
	cmd.Env = append(cmd.Environ(), "GIT_CONFIG_GLOBAL="+config)
	out, err := cmd.Output()
	require.NoError(t, err, "git reads no %s from %s", key, config)
	return strings.TrimSpace(string(out))
}

// "another thing i want it to have is its own git user so it can develop and create branches and so on."
func TestTheRootAgentHasAGitUserOfItsOwn(t *testing.T) {
	s, ran := runningTheRootAgent(t, opusLow)
	_, refused := saying(s, sigil.Sent{"says": "do you have git?"})
	require.Nil(t, refused)

	config := filepath.Join(s.rootAgent.home, "gitconfig")
	env := ranWith(t, ran, "0", "env")
	assert.Contains(t, env, "GIT_CONFIG_GLOBAL="+config)
	// What its git asks the node, it asks as itself.
	assert.Contains(t, env, "QNTX_TOKEN="+s.rootAgent.token)

	// Its author is itself: its DID, at the node it is the agent of.
	assert.Equal(t, "ROOT agent", gitSays(t, config, "user.name"))
	key, isKey := strings.CutPrefix(s.rootAgent.did, "did:key:")
	require.True(t, isKey)
	assert.Equal(t, key+"@localhost", gitSays(t, config, "user.email"))

	// It holds no token: its git asks the node when it pushes.
	helper := gitSays(t, config, "credential.https://github.com.helper")
	assert.Contains(t, helper, "git credential")
	assert.Contains(t, helper, "http://127.0.0.1:8770")
	assert.Equal(t, "true", gitSays(t, config, "credential.https://github.com.useHttpPath"))
}

// git itself, reading the configuration the node writes, runs the helper it
// names with the node's address, says which repository it is reaching, and
// takes what the helper hands back.
func TestGitAsksTheHelperTheNodeConfigured(t *testing.T) {
	dir := t.TempDir()
	// A stand-in for the node's own binary, in a directory with a space in it.
	bin := filepath.Join(dir, "the node", "qntx")
	require.NoError(t, os.MkdirAll(filepath.Dir(bin), 0o755))
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\n"+
		"printf '%s\\n' \"$@\" > '"+dir+"/args'\n"+
		"cat > '"+dir+"/asked'\n"+
		"printf 'username=x-access-token\\npassword=ghs_minted\\n'\n"), 0o755))

	agent := &rootAgent{home: filepath.Join(dir, "home")}
	config, err := agent.writeGitConfig("QNTX ROOT agent", "z6MkAgent@localhost", bin, "http://127.0.0.1:8770")
	require.NoError(t, err)

	fill := exec.Command("git", "credential", "fill")
	fill.Env = append(fill.Environ(), "GIT_CONFIG_GLOBAL="+config, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
	fill.Stdin = strings.NewReader("url=https://github.com/teranos/QNTX.git\n\n")
	filled, err := fill.CombinedOutput()
	require.NoError(t, err, "git said: %s", filled)
	assert.Contains(t, string(filled), "password=ghs_minted")

	args, err := os.ReadFile(filepath.Join(dir, "args"))
	require.NoError(t, err)
	assert.Equal(t, "git\ncredential\n--to\nhttp://127.0.0.1:8770\nget\n", string(args))
	asked, err := os.ReadFile(filepath.Join(dir, "asked"))
	require.NoError(t, err)
	assert.Contains(t, string(asked), "host=github.com\n")
	assert.Contains(t, string(asked), "path=teranos/QNTX.git\n")
}

// The address is its DID at the host the node answers on, and its name says
// which node's ROOT agent it is when the node has a name.
func TestAnAgentsGitIdentityIsItsDIDAtItsNode(t *testing.T) {
	name, email := gitIdentityOf("did:key:z6MkAgent", "QNTX", "https://api.example.org")
	assert.Equal(t, "QNTX ROOT agent", name)
	assert.Equal(t, "z6MkAgent@api.example.org", email)

	name, email = gitIdentityOf("did:key:z6MkAgent", "", "")
	assert.Equal(t, "ROOT agent", name)
	assert.Equal(t, "z6MkAgent@localhost", email)
}
