package server

// "another thing i want is that rituals run on the box from now on, not locally on my machine per se."

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/sigil"
)

// Rituals are switched on by what is said in the place they stand (ground source/ritual/onoff.d),
// and grove's stand in the Node Agent's grove checkout (ADR-050, Not done). Whoever speaks names
// the repository of its work the turn is said in, and it is said there, in its one session.
func TestATurnIsSaidInTheRepositoryOfItsWorkTheSpeakerNames(t *testing.T) {
	s, ran := runningTheNodeAgent(t, opusLow)
	grove := filepath.Join(s.nodeAgent.home, "work", "sbvh-nl", "grove")
	require.NoError(t, os.MkdirAll(filepath.Join(grove, ".git"), 0o755))

	_, refused := saying(s, sigil.Sent{"says": "hello"})
	require.Nil(t, refused)
	_, refused = saying(s, sigil.Sent{"says": "rituals on", "in": "sbvh-nl/grove"})
	require.Nil(t, refused)

	in, err := filepath.EvalSymlinks(grove)
	require.NoError(t, err)
	at, err := filepath.EvalSymlinks(ranWith(t, ran, "1", "cwd")[0])
	require.NoError(t, err)
	assert.Equal(t, in, at)
	// The same session, resumed: what is said in the checkout is said to the one Node Agent.
	assert.Equal(t, after(ranWith(t, ran, "0", "args"), "--session-id"), after(ranWith(t, ran, "1", "args"), "--resume"))

	work, err := filepath.EvalSymlinks(filepath.Join(s.nodeAgent.home, "work"))
	require.NoError(t, err)
	at, err = filepath.EvalSymlinks(ranWith(t, ran, "0", "cwd")[0])
	require.NoError(t, err)
	assert.Equal(t, work, at, "a turn that names no repository is said in its work")
}

// The say signum says it takes in, so whoever speaks can find it.
func TestSayTakesTheRepositoryATurnIsSaidIn(t *testing.T) {
	s, _ := runningTheNodeAgent(t, opusLow)
	names := []string{}
	for _, param := range s.claudeHarness().sayTakes {
		names = append(names, param.GetName())
	}
	assert.Contains(t, names, "in")
}
