package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appcfg "github.com/teranos/QNTX/internal/config"
)

func reachingConfig(lines ...appcfg.ReachConfig) *appcfg.Config {
	cfg := &appcfg.Config{}
	cfg.Auth.Reach = lines
	return cfg
}

// 「[Opus 5.5] auth.reach is read as the store's REACH lines are: paths, and the roles」
// 「[Opus 5.5] that reach them, by am.toml.」
func TestAuthReachIsReadAsReachLines(t *testing.T) {
	lines, err := declaredReach(reachingConfig(
		appcfg.ReachConfig{Paths: []string{"/a2a/"}, To: []string{"ground"}},
		appcfg.ReachConfig{Paths: []string{"/am/statusline/{name}"}, To: []string{"GROUND"}},
	))
	require.NoError(t, err)
	require.Len(t, lines, 2)
	assert.Equal(t, []string{"/a2a/"}, lines[0].Paths)
	assert.Equal(t, []string{"GROUND"}, lines[0].Roles, "a role is upper case, as the store's lines read it")
	assert.Equal(t, declaredReachActor, lines[1].Actor)

	none, err := declaredReach(reachingConfig())
	require.NoError(t, err)
	assert.Empty(t, none, "saying none opens nothing")
}

// 「[Opus 5.5] A line that would not be served from the store is refused here, and the」
// 「[Opus 5.5] whole list with it: no path, no role, ROOT, or a level.」
func TestAuthReachRefusesWhatTheStoreWouldNotServe(t *testing.T) {
	for name, line := range map[string]appcfg.ReachConfig{
		"no path": {To: []string{"GROUND"}},
		"no role": {Paths: []string{"/a2a/"}},
		"ROOT":    {Paths: []string{"/a2a/"}, To: []string{"ROOT"}},
		"a level": {Paths: []string{"/a2a/"}, To: []string{"PUBLIC_REGISTRATION"}},
	} {
		_, err := declaredReach(reachingConfig(appcfg.ReachConfig{Paths: []string{"/a2a/"}, To: []string{"GROUND"}}, line))
		assert.Error(t, err, name)
	}
}
