package forge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestARepoOnAForgeTheNodeDoesNotReachIsRefusedNamingIt(t *testing.T) {
	gitlab, err := ParseRepo("gitlab.com/group/sub/project")
	require.NoError(t, err)
	_, err = ForgesOf(map[string]Forge{}).For(gitlab)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gitlab.com")
	assert.Contains(t, err.Error(), "gitlab.com/group/sub/project")
}

// Qodo's review of #1515: a host is a name case does not tell apart, as the
// GitHub adapter reads it. GitHub.com/owner/repo parsed, and the forge on
// github.com was not found for it.
func TestAHostIsFoundWhateverItsCase(t *testing.T) {
	on := &shelf{}
	repo, err := ParseRepo("GitHub.com/teranos/QNTX")
	require.NoError(t, err)
	found, err := ForgesOf(map[string]Forge{GitHub: on}).For(repo)
	require.NoError(t, err)
	assert.Same(t, on, found)
}

func TestActingStartsAboveZero(t *testing.T) {
	var nobody Acting
	assert.NotEqual(t, AsNode, nobody)
	assert.NotEqual(t, AsApp, nobody)
	assert.Equal(t, "as nobody", nobody.String())
}
