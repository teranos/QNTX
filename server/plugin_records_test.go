package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
)

// "+", a repository URL, confirm: the plugin is known and starts disabled.
func TestAnAddedPluginStartsDisabled(t *testing.T) {
	s := rootKnowingServer(t)
	records := s.pluginRecords()

	added, err := records.AddPlugin(rootAccount, "https://github.com/teranos/pyre")
	require.NoError(t, err)
	assert.Equal(t, "pyre", added.Name)
	assert.False(t, added.Enabled)

	held, found, err := records.Plugin("pyre")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "https://github.com/teranos/pyre", held.Repo)
	assert.False(t, held.Enabled)
}

// A tree URL names the plugin by its last segment.
func TestATreeURLNamesThePluginByItsLastSegment(t *testing.T) {
	s := rootKnowingServer(t)
	added, err := s.pluginRecords().AddPlugin(rootAccount, "https://github.com/teranos/QNTX/tree/main/qntx-plugins/loom")
	require.NoError(t, err)
	assert.Equal(t, "loom", added.Name)
}

// What is entered has to be a repository URL.
func TestAddingSomethingThatIsNotARepositoryIsRefused(t *testing.T) {
	s := rootKnowingServer(t)
	_, err := s.pluginRecords().AddPlugin(rootAccount, "pyre")
	require.Error(t, err)
}

// A plugin is added once.
func TestAPluginIsAddedOnce(t *testing.T) {
	s := rootKnowingServer(t)
	records := s.pluginRecords()
	_, err := records.AddPlugin(rootAccount, "https://github.com/teranos/pyre")
	require.NoError(t, err)
	_, err = records.AddPlugin(rootAccount, "https://github.com/teranos/pyre")
	require.Error(t, err)
}

// Config and enabling are written as new lines, and the newest line about a
// plugin is what the node holds.
func TestTheNewestLineAboutAPluginHolds(t *testing.T) {
	s := rootKnowingServer(t)
	records := s.pluginRecords()
	_, err := records.AddPlugin(rootAccount, "https://github.com/teranos/pyre")
	require.NoError(t, err)

	time.Sleep(2 * time.Millisecond)
	require.NoError(t, records.ConfigurePlugin(rootAccount, "pyre", map[string]string{"poll_interval": "300"}))
	time.Sleep(2 * time.Millisecond)
	require.NoError(t, records.EnablePlugin(rootAccount, "pyre", true))

	held, found, err := records.Plugin("pyre")
	require.NoError(t, err)
	require.True(t, found)
	assert.True(t, held.Enabled, "enabling keeps the config")
	assert.Equal(t, map[string]string{"poll_interval": "300"}, held.Config)
	assert.Equal(t, "https://github.com/teranos/pyre", held.Repo)

	all, err := records.Plugins()
	require.NoError(t, err)
	assert.Equal(t, []grpcplugin.PluginRecord{held}, all)
}

// Enabling and configuring are for a plugin that was added.
func TestAPluginNobodyAddedIsRefused(t *testing.T) {
	s := rootKnowingServer(t)
	records := s.pluginRecords()
	require.Error(t, records.EnablePlugin(rootAccount, "pyre", true))
	require.Error(t, records.ConfigurePlugin(rootAccount, "pyre", map[string]string{"a": "b"}))
}
