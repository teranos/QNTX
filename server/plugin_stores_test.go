package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
)

// A plugin standing in a namespace is handed a token reaching that namespace's
// store, a new one at each Initialize with the one before it spent, and never
// one onto system (ADR-046).
func TestAPluginsTokenReachesTheNamespaceItStandsIn(t *testing.T) {
	srv, _ := pluginServingServer(t, "other")
	served := srv.held.ServedUniverse().Name()

	first, err := srv.pluginToken("cleanAPI", served)
	require.NoError(t, err)
	reached, standing := srv.storeOfPlugin(first)
	require.True(t, standing, "the plugin's token reached nothing")
	assert.Same(t, srv.held.Served(), reached, "the token reached another store than the namespace named")

	second, err := srv.pluginToken("cleanAPI", served)
	require.NoError(t, err)
	_, still := srv.storeOfPlugin(first)
	assert.False(t, still, "the token of the Initialize before outlived it")
	_, standing = srv.storeOfPlugin(second)
	assert.True(t, standing)

	_, err = srv.pluginToken("cleanAPI", auth.NamespaceSystem)
	require.Error(t, err, "a plugin was handed a token onto the node's own records")
	_, standing = srv.storeOfPlugin(second)
	assert.True(t, standing, "a refused mint spent the token the plugin held")
}
