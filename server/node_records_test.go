package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GitHub is on for the node until somebody switches it off, and the runner is
// off until somebody switches it on, at /opt/actions-runner unless told.
func TestTheNodesGitHubStartsOnAndItsRunnerOff(t *testing.T) {
	s := rootKnowingServer(t)
	records := s.nodeRecords()

	settings, err := records.GitHub()
	require.NoError(t, err)
	assert.True(t, settings.Enabled)
	assert.False(t, settings.RunnerEnabled)
	assert.Equal(t, "/opt/actions-runner", settings.RunnerPath)

	time.Sleep(2 * time.Millisecond)
	require.NoError(t, records.SetGitHub(rootAccount, GitHubSettings{Enabled: false, RunnerPath: "/srv/runner", RunnerEnabled: true}))
	settings, err = records.GitHub()
	require.NoError(t, err)
	assert.False(t, settings.Enabled)
	assert.True(t, settings.RunnerEnabled)
	assert.Equal(t, "/srv/runner", settings.RunnerPath)
}
