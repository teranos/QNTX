package server

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/sigil"
)

// Everything accessible through signa is there for a subsystem to call: it
// asks a sigil from inside the node, over the subsystem surface, the node's
// own as well as its own signum's.
func TestASubsystemAsksASigilTheNodeServes(t *testing.T) {
	_, f := linkedServer(t)

	answered, refusal := f.host.Ask(context.Background(), linkedName, "ping", nil)
	require.Nil(t, refusal)
	assert.IsType(t, &protocol.Empty{}, answered)

	answered, refusal = f.host.Ask(context.Background(), "am", "version", nil)
	require.Nil(t, refusal, "a sigil of the node's own is asked as a subsystem's is")
	assert.NotNil(t, answered)
}

// What it sends is read and refused as on every surface.
func TestWhatASubsystemSendsIsReadAsEverySurfaceReadsIt(t *testing.T) {
	_, f := linkedServer(t)

	_, refusal := f.host.Ask(context.Background(), "embeddings", "generate", map[string]any{})
	require.NotNil(t, refusal)
	assert.Equal(t, sigil.Missing, refusal.GetWhy())
	assert.Equal(t, "text", refusal.GetParam())

	_, refusal = f.host.Ask(context.Background(), "vectors", "search", map[string]any{"index": "i", "vector": "not a list", "top_k": 1})
	require.NotNil(t, refusal)
	assert.Equal(t, sigil.Invalid, refusal.GetWhy())
}

// A sigil the node does not serve is not found, by its name.
func TestASigilTheNodeDoesNotServeIsNotFound(t *testing.T) {
	_, f := linkedServer(t)

	_, refusal := f.host.Ask(context.Background(), "nowhere", "nothing", nil)
	require.NotNil(t, refusal)
	assert.Equal(t, sigil.NotFound, refusal.GetWhy())
}

// A provided service the node runs no router for says so, and does not
// pretend to answer.
func TestAProvidedServiceWithoutItsRouterSaysSo(t *testing.T) {
	_, f := linkedServer(t)

	_, refusal := f.host.Ask(context.Background(), "vectors", "search",
		map[string]any{"index": "embeddings.m", "vector": []float32{1, 0}, "top_k": 3})
	require.NotNil(t, refusal)
	assert.Equal(t, sigil.Failed, refusal.GetWhy())
	assert.Contains(t, refusal.GetSays(), "vector search")
}
