package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	errors "github.com/teranos/sacred-error"
)

// deployingServer is a node whose deploys write their lines where it keeps
// its own.
func deployingServer(t *testing.T) *QNTXServer {
	t.Helper()
	s := rootKnowingServer(t)
	s.deploys.writesWith(s.deployLine)
	return s
}

// A push queues the deploy, and the build that takes it up is the same deploy:
// its lines are all about the plugin at the commit pushed.
func TestAPushedDeployIsOneDeployFromQueuedToFailed(t *testing.T) {
	s := deployingServer(t)
	s.deploys.begin(pluginDeploy{plugin: "datapunt", rev: "c0ffee", repo: "teranos/datapunt", ref: "refs/heads/main", after: "c0ffee", stage: deployQueued})
	require.True(t, s.deploys.inFlight("datapunt"))

	s.deploys.begin(pluginDeploy{plugin: "datapunt", rev: "c0ffee", replacesRevs: []string{"beef", "f00d"}, revs: []string{"c0ffee", "f00d"}, stage: deployBuilding})
	s.deploys.fail("datapunt", errors.New("dub build: <exit 1>"))
	assert.False(t, s.deploys.inFlight("datapunt"), "a failed deploy is still in flight")

	held := s.deploys.active()
	require.Len(t, held, 1)
	d := held[0]
	assert.Equal(t, "teranos/datapunt", d.repo, "the build forgot the push it took up")
	assert.Equal(t, "c0ffee", d.after)
	assert.Equal(t, []string{"beef", "f00d"}, d.replacesRevs)
	assert.Equal(t, []string{"c0ffee", "f00d"}, d.revs)
	assert.Equal(t, deployFailed, d.stage)
	assert.Equal(t, deployBuilding, d.failedAt)
	assert.Equal(t, "dub build: <exit 1>", d.failure)
	for _, stage := range []string{deployQueued, deployBuilding, deployFailed} {
		assert.Contains(t, d.began, stage, "when %s began is not held", stage)
	}
	assert.Empty(t, d.unwritten)

	lines, err := newestLines(s, "datapunt@c0ffee", "deploys")
	require.NoError(t, err)
	for _, predicate := range []string{deployPredicate + deployQueued, deployPredicate + deployBuilding, deployPredicate + deployFailed} {
		assert.Contains(t, lines, predicate, "no %s line about datapunt@c0ffee", predicate)
	}
	assert.Equal(t, deployBuilding, lines[deployPredicate+deployFailed].Attributes["failed_at"])
}

// A build at the revs already installed lands nothing, and the deploy ends.
func TestADeployThatChangesNothingEnds(t *testing.T) {
	s := deployingServer(t)
	s.deploys.begin(pluginDeploy{plugin: "inbox", rev: "c0ffee", revs: []string{"c0ffee"}, stage: deployBuilding})
	s.deploys.unchanged("inbox")
	assert.False(t, s.deploys.inFlight("inbox"))

	lines, err := newestLines(s, "inbox@c0ffee", "deploys")
	require.NoError(t, err)
	assert.Contains(t, lines, deployPredicate+deployUnchanged)
}

// A check run is written on the deploy of the commit it ran on, and on no other.
func TestACheckLandsOnTheDeployOfItsCommit(t *testing.T) {
	s := deployingServer(t)
	s.deploys.begin(pluginDeploy{plugin: "datapunt", rev: "c0ffee", repo: "teranos/datapunt", ref: "refs/heads/main", after: "c0ffee", stage: deployQueued})
	s.deploys.begin(pluginDeploy{plugin: "inbox", rev: "beef", repo: "teranos/QNTX", ref: "refs/heads/main", after: "beef", stage: deployQueued})

	s.deploys.check("teranos/datapunt", "c0ffee", "Go", checkRunning)
	s.deploys.check("teranos/datapunt", "c0ffee", "Go", checkDone)

	for _, d := range s.deploys.active() {
		switch d.plugin {
		case "datapunt":
			assert.Equal(t, map[string]string{"Go": checkDone}, d.checks)
		case "inbox":
			assert.Empty(t, d.checks, "a check on teranos/datapunt landed on inbox")
		}
	}
	lines, err := newestLines(s, "datapunt@c0ffee", "deploys")
	require.NoError(t, err)
	assert.Equal(t, checkDone, lines[deployPredicate+deployChecked].Attributes["state"])
}

// What active hands out is a copy: the statusline reading it changes nothing.
func TestWhatActiveHandsOutIsACopy(t *testing.T) {
	s := deployingServer(t)
	s.deploys.begin(pluginDeploy{plugin: "datapunt", rev: "c0ffee", repo: "teranos/datapunt", after: "c0ffee", stage: deployQueued})
	s.deploys.check("teranos/datapunt", "c0ffee", "Go", checkRunning)

	held := s.deploys.active()
	held[0].checks["Go"] = checkFailed
	held[0].stage = deployLive

	again := s.deploys.active()
	assert.Equal(t, checkRunning, again[0].checks["Go"])
	assert.Equal(t, deployQueued, again[0].stage)
}

// A build the runner delivered is a deploy of its own, about the archive it
// installed, and a plugin that is not enabled ends it.
func TestABuildLandingWithNoDeployInFlightBeginsOne(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := deployingServer(t)
	dir, err := grpcplugin.PluginInstallPath("inbox")
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	digest := strings.Repeat("ab", 32)
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".installed.sha256"), []byte(digest), 0o644))

	s.buildLanded("inbox")

	held := s.deploys.active()
	require.Len(t, held, 1)
	assert.Equal(t, digest, held[0].rev)
	assert.Equal(t, deployDisabled, held[0].stage)
	assert.Contains(t, held[0].began, deployRestarting)
}

// A line that is not written is said on the deploy, which carries on.
func TestALineNotWrittenIsHeldOnTheDeploy(t *testing.T) {
	s := deployingServer(t)
	s.deploys.write = func(subject, predicate, plugin string, attributes map[string]any) error {
		return errors.Newf("the store refused %s about %s", predicate, subject)
	}
	s.deploys.begin(pluginDeploy{plugin: "inbox", rev: "c0ffee", stage: deployBuilding})
	s.deploys.stage("inbox", deployRestarting)

	held := s.deploys.active()
	require.Len(t, held, 1)
	assert.Equal(t, deployRestarting, held[0].stage)
	assert.Len(t, held[0].unwritten, 2)
}
