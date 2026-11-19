package server

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// #1641 as GitHub said it merged: harness-pi into main, in teranos/QNTX.
const mergedHarnessPi = `{"action":"closed","number":1641,"pull_request":{"merged":true,"head":{"ref":"harness-pi","repo":{"full_name":"teranos/QNTX"}},"base":{"ref":"main"}},"repository":{"full_name":"teranos/QNTX"}}`

// "why would QNTX not just be able to know it did merge, and adjust accordingly ?"
func TestAPluginOnAMergedBranchFollowsItIntoTheBase(t *testing.T) {
	s := rootKnowingServer(t)
	records := s.pluginRecords()
	_, err := records.AddPlugin(rootAccount, "https://github.com/teranos/QNTX/tree/harness-pi/plugins/claude")
	require.NoError(t, err)
	require.NoError(t, records.EnablePlugin(rootAccount, "claude", true))
	require.NoError(t, records.ConfigurePlugin(rootAccount, "claude", map[string]string{"namespace": "default"}))

	followed, err := s.pluginsFollowMerge("pull_request", []byte(mergedHarnessPi))
	require.NoError(t, err)
	assert.Equal(t, []string{"claude"}, followed)

	held, found, err := records.Plugin("claude")
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "https://github.com/teranos/QNTX/tree/main/plugins/claude", held.Repo)
	assert.True(t, held.Enabled)
	assert.Equal(t, map[string]string{"namespace": "default"}, held.Config)
}

// A plugin on another branch, or in another repository, stays where it is.
func TestAPluginOnAnotherBranchStays(t *testing.T) {
	s := rootKnowingServer(t)
	records := s.pluginRecords()
	_, err := records.AddPlugin(rootAccount, "https://github.com/teranos/QNTX/tree/l-stack/plugins/claude")
	require.NoError(t, err)
	_, err = records.AddPlugin(rootAccount, "https://github.com/teranos/pyre/tree/harness-pi/pyre")
	require.NoError(t, err)

	followed, err := s.pluginsFollowMerge("pull_request", []byte(mergedHarnessPi))
	require.NoError(t, err)
	assert.Empty(t, followed)

	held, _, err := records.Plugin("claude")
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/teranos/QNTX/tree/l-stack/plugins/claude", held.Repo)
}

// Closed without merging, the branch's work is not in the base.
func TestAPullRequestClosedUnmergedMovesNothing(t *testing.T) {
	s := rootKnowingServer(t)
	_, err := s.pluginRecords().AddPlugin(rootAccount, "https://github.com/teranos/QNTX/tree/harness-pi/plugins/claude")
	require.NoError(t, err)

	followed, err := s.pluginsFollowMerge("pull_request", []byte(`{"action":"closed","pull_request":{"merged":false,"head":{"ref":"harness-pi","repo":{"full_name":"teranos/QNTX"}},"base":{"ref":"main"}},"repository":{"full_name":"teranos/QNTX"}}`))
	require.NoError(t, err)
	assert.Empty(t, followed)
}

// A head on a fork names the fork's branch, not one of the repository merged into.
func TestAMergeFromAForkMovesNothing(t *testing.T) {
	s := rootKnowingServer(t)
	_, err := s.pluginRecords().AddPlugin(rootAccount, "https://github.com/teranos/QNTX/tree/harness-pi/plugins/claude")
	require.NoError(t, err)

	followed, err := s.pluginsFollowMerge("pull_request", []byte(`{"action":"closed","pull_request":{"merged":true,"head":{"ref":"harness-pi","repo":{"full_name":"someone/QNTX"}},"base":{"ref":"main"}},"repository":{"full_name":"teranos/QNTX"}}`))
	require.NoError(t, err)
	assert.Empty(t, followed)
}
