package comet

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ground's libacp branch was merged into its main and is gone from GitHub, and main holds make sand
// (ground Makefile, sand:): an agent's ground, and every comet, is built from ground's main.
func TestAnAgentsGroundIsBuiltFromGroundsMain(t *testing.T) {
	node := engineNode{builds: t.TempDir(), asked: make(chan Source, 1)}
	err := Builder{Node: node, NixBin: "/nix/bin"}.BuildAgent(context.Background(), t.TempDir(), "http://127.0.0.1:8772")
	require.Error(t, err)
	assert.Equal(t, Source{Owner: "teranos", Repo: "ground", Branch: "main"}, <-node.asked)
}
