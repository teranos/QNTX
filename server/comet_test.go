package server

// "the feature i am imagining is called comet"

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCometIsNamedByItsRepository(t *testing.T) {
	owner, repo, err := cometOf("/comet/abcd-nl/grove")
	require.NoError(t, err)
	assert.Equal(t, "abcd-nl", owner)
	assert.Equal(t, "grove", repo)

	for _, path := range []string{"/comet/", "/comet/abcd-nl", "/comet/abcd-nl/", "/comet/abcd-nl/grove/more", "/comet//grove", "/comet/../grove"} {
		_, _, err := cometOf(path)
		assert.Error(t, err, path)
	}
}

func TestCometSandIsGroundsControlsAndTheProjectsAlone(t *testing.T) {
	// One project block naming the checkout: wind folds that project's controls and nothing of anyone's.
	assert.Equal(t, "project {\n  path: \"/b/grove\"\n}\n", cometDeclaration("/b/grove"))
}
