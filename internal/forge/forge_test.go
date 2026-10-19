package forge

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestARepoNamesItsForge(t *testing.T) {
	for said, want := range map[string]Repo{
		"github.com/teranos/QNTX":             {Host: "github.com", Path: "teranos/QNTX"},
		"github.com/teranos/QNTX.git":         {Host: "github.com", Path: "teranos/QNTX"},
		"gitlab.com/group/subgroup/project":   {Host: "gitlab.com", Path: "group/subgroup/project"},
		" gitlab.example.org/group/project/ ": {Host: "gitlab.example.org", Path: "group/project"},
	} {
		got, err := ParseRepo(said)
		require.NoError(t, err, said)
		assert.Equal(t, want, got, said)
	}
}

// "owner/repo" says which forge it is on to nobody.
func TestARepoWithNoHostIsRefused(t *testing.T) {
	for _, said := range []string{"teranos/QNTX", "QNTX", "", "github.com", "github.com/teranos", "github.com//QNTX", "github.com/teranos//QNTX"} {
		_, err := ParseRepo(said)
		assert.Error(t, err, "%q", said)
	}
}

func TestTheSameNameOnTwoForgesIsTwoRepositories(t *testing.T) {
	github, err := ParseRepo("github.com/teranos/QNTX")
	require.NoError(t, err)
	gitlab, err := ParseRepo("gitlab.com/teranos/QNTX")
	require.NoError(t, err)
	assert.False(t, github.Is(gitlab))
	again, err := ParseRepo("GitHub.com/Teranos/qntx")
	require.NoError(t, err)
	assert.True(t, github.Is(again))
	assert.Equal(t, "github.com/teranos/QNTX", github.String())
}

func TestAPushSaysWhereItWentAndWhatItChanged(t *testing.T) {
	push := Push{Ref: "refs/heads/main", Changed: []string{"notes/a.md", "README.md"}}
	assert.Equal(t, "main", push.Branch())
	assert.Equal(t, "", push.Tag())
	assert.True(t, push.Touched("README.md"))
	assert.False(t, push.Touched("notes"))
	assert.True(t, push.TouchedUnder("notes"))
	assert.False(t, push.TouchedUnder("note"))

	tagged := Push{Ref: "refs/tags/v1.2.3"}
	assert.Equal(t, "", tagged.Branch())
	assert.Equal(t, "v1.2.3", tagged.Tag())
}
