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

// heldDeploy is the deploy the tracker holds for plugin.
func heldDeploy(t *testing.T, s *QNTXServer, plugin string) *pluginDeploy {
	t.Helper()
	s.deploys.mu.Lock()
	defer s.deploys.mu.Unlock()
	d, held := s.deploys.deploys[plugin]
	require.True(t, held, "no deploy of %s is held", plugin)
	return d
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

	d := heldDeploy(t, s, "datapunt")
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

// A build ended from outside it ends its deploy as interrupted, not failed,
// at the stage it was at.
func TestAnInterruptedBuildEndsItsDeployAsInterrupted(t *testing.T) {
	s := deployingServer(t)
	s.deploys.begin(pluginDeploy{plugin: "cleanAPI", rev: "c0ffee", revs: []string{"c0ffee"}, stage: deployBuilding})
	s.deploys.interrupt("cleanAPI", errors.New("Compiling writeable v0.6.4: signal: terminated"))
	assert.False(t, s.deploys.inFlight("cleanAPI"), "an interrupted deploy is still in flight")

	d := heldDeploy(t, s, "cleanAPI")
	assert.Equal(t, deployInterrupted, d.stage)
	assert.Equal(t, deployBuilding, d.failedAt)

	lines, err := newestLines(s, "cleanAPI@c0ffee", "deploys")
	require.NoError(t, err)
	assert.NotContains(t, lines, deployPredicate+deployFailed)
	require.Contains(t, lines, deployPredicate+deployInterrupted)
	assert.Equal(t, deployBuilding, lines[deployPredicate+deployInterrupted].Attributes["interrupted_at"])
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

	d := heldDeploy(t, s, "inbox")
	assert.Equal(t, digest, d.rev)
	assert.Equal(t, deployDisabled, d.stage)
	assert.Contains(t, d.began, deployRestarting)
}

// A line that is not written is said on the deploy, which carries on.
func TestALineNotWrittenIsHeldOnTheDeploy(t *testing.T) {
	s := deployingServer(t)
	s.deploys.write = func(subject, predicate, plugin string, attributes map[string]any) error {
		return errors.Newf("the store refused %s about %s", predicate, subject)
	}
	s.deploys.begin(pluginDeploy{plugin: "inbox", rev: "c0ffee", stage: deployBuilding})
	s.deploys.stage("inbox", deployRestarting)

	d := heldDeploy(t, s, "inbox")
	assert.Equal(t, deployRestarting, d.stage)
	assert.Len(t, d.unwritten, 2)
}

// A deploy's lines are about the plugin at its short rev, a name and not an
// identifier; the full rev is on the line that begins it.
func TestADeployIsAboutThePluginAtItsShortRev(t *testing.T) {
	s := deployingServer(t)
	after := "8ed95ffc2a6b0d4e1f3a5c7b9d0e2f4a6c8b0d1e"
	s.deploys.begin(pluginDeploy{plugin: "datapunt", rev: after, after: after, stage: deployQueued})

	lines, err := newestLines(s, "datapunt@8ed95ff", "deploys")
	require.NoError(t, err)
	require.Contains(t, lines, deployPredicate+deployQueued, "no queued line about datapunt@8ed95ff")
	queued := lines[deployPredicate+deployQueued]
	assert.Equal(t, []string{"datapunt@8ed95ff"}, queued.Subjects)
	assert.Equal(t, after, queued.Attributes["rev"])
	assert.Equal(t, after, queued.Attributes["after"])
}
