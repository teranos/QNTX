package forge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestARepoOnAForgeTheNodeDoesNotReadIsRefusedNamingIt(t *testing.T) {
	gitlab, err := ParseRepo("gitlab.com/group/sub/project")
	require.NoError(t, err)
	_, err = ReadersOf(map[string]Reader{}).For(gitlab)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gitlab.com")
	assert.Contains(t, err.Error(), "gitlab.com/group/sub/project")
}

func TestActingStartsAboveZero(t *testing.T) {
	var nobody Acting
	assert.NotEqual(t, AsNode, nobody)
	assert.NotEqual(t, AsApp, nobody)
	assert.Equal(t, "as nobody", nobody.String())
}
