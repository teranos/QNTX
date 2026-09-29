package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// buildArchive is what a plugin's package step writes, for this platform.
func buildArchive(name, version string) string {
	return "qntx-" + name + "-plugin-" + version + pluginAssetSuffix()
}

// landBuild writes an archive and its .sha256 into a job's workspace the way
// the package step does: the archive, then the digest beside it.
func landBuild(t *testing.T, workspace, name, version string, binary []byte, digestOverride string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(workspace, 0o755))
	archive := tarGz(t, map[string][]byte{PluginBinaryName(name): binary})
	file := buildArchive(name, version)
	require.NoError(t, os.WriteFile(filepath.Join(workspace, file), archive, 0o644))
	sum := sha256.Sum256(archive)
	digest := hex.EncodeToString(sum[:])
	if digestOverride != "" {
		digest = digestOverride
	}
	require.NoError(t, os.WriteFile(filepath.Join(workspace, file+".sha256"), []byte(digest+"  "+file+"\n"), 0o644))
}

// aRunner is a directory that holds a registered runner.
func aRunner(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".runner"), []byte(`{"agentName":"q-api-box","gitHubUrl":"https://github.com/org"}`), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "_work"), 0o755))
	return dir
}

func TestAPluginIsNamedByItsBuild(t *testing.T) {
	name, ok := BuildPlugin(buildArchive("openrouter-qntx", "1.2.3"))
	assert.True(t, ok)
	assert.Equal(t, "openrouter-qntx", name)

	_, ok = BuildPlugin("qntx-duif-plugin-1.0.0-plan9-mips.tar.gz")
	assert.False(t, ok, "a build for another platform is not this node's")
	_, ok = BuildPlugin("notes.tar.gz")
	assert.False(t, ok)
}

// "it errors when there is no runner"
func TestAPathWithNoRunnerIsRefused(t *testing.T) {
	_, err := OpenRunner(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), ".runner")
}

// "A plugin's new build lands under the runner, and QNTX has it running as fast
// as it can. Nothing polls."
func TestABuildLandingUnderTheRunnerIsInstalled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	logger := zaptest.NewLogger(t).Sugar()
	runner, err := OpenRunner(aRunner(t))
	require.NoError(t, err)

	landed := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, runner.Watch(ctx, func(name string) { landed <- name }, logger))

	workspace := filepath.Join(runner.Path(), "_work", "pyre", "pyre")
	landBuild(t, workspace, "pyre", "1.0.0", []byte("\x7fELF pyre"), "")

	select {
	case name := <-landed:
		assert.Equal(t, "pyre", name)
	case <-time.After(5 * time.Second):
		t.Fatal("the build landed and nothing was installed")
	}

	dir, err := PluginInstallPath("pyre")
	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Join(dir, PluginBinaryName("pyre")))
	require.NoError(t, err)
	assert.Equal(t, []byte("\x7fELF pyre"), got)
}

// A build already under the runner when its directory is first watched raised
// no event, and is taken all the same.
func TestABuildAlreadyUnderTheRunnerIsTaken(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	logger := zaptest.NewLogger(t).Sugar()
	runner, err := OpenRunner(aRunner(t))
	require.NoError(t, err)
	landBuild(t, filepath.Join(runner.Path(), "_work", "pyre", "pyre"), "pyre", "1.0.0", []byte("\x7fELF pyre"), "")

	landed := make(chan string, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, runner.Watch(ctx, func(name string) { landed <- name }, logger))

	select {
	case name := <-landed:
		assert.Equal(t, "pyre", name)
	case <-time.After(5 * time.Second):
		t.Fatal("a build already under the runner was not taken")
	}
}

// A build whose .sha256 disagrees with it is not installed.
func TestABuildThatDisagreesWithItsDigestIsNotInstalled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	logger := zaptest.NewLogger(t).Sugar()
	runner, err := OpenRunner(aRunner(t))
	require.NoError(t, err)

	workspace := filepath.Join(runner.Path(), "_work", "pyre", "pyre")
	landBuild(t, workspace, "pyre", "1.0.0", []byte("\x7fELF pyre"), "0000000000000000000000000000000000000000000000000000000000000000")

	_, err = runner.Take(filepath.Join(workspace, buildArchive("pyre", "1.0.0")+".sha256"), logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "digest")

	dir, err := PluginInstallPath("pyre")
	require.NoError(t, err)
	_, err = os.Stat(dir)
	assert.True(t, os.IsNotExist(err), "nothing was installed")
}

// A build already installed is not installed again, so a plugin is not
// restarted for nothing.
func TestTheSameBuildTakenTwiceChangesNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	logger := zaptest.NewLogger(t).Sugar()
	runner, err := OpenRunner(aRunner(t))
	require.NoError(t, err)

	workspace := filepath.Join(runner.Path(), "_work", "pyre", "pyre")
	landBuild(t, workspace, "pyre", "1.0.0", []byte("\x7fELF pyre"), "")
	digestFile := filepath.Join(workspace, buildArchive("pyre", "1.0.0")+".sha256")

	first, err := runner.Take(digestFile, logger)
	require.NoError(t, err)
	assert.True(t, first.Changed)
	second, err := runner.Take(digestFile, logger)
	require.NoError(t, err)
	assert.False(t, second.Changed)

	shown := runner.Stats().Taken
	require.Len(t, shown, 1, "one build is one row, however often it is seen")
	assert.True(t, shown[0].Changed, "it is the build that changed the plugin")
}

// A build found under the runner that is already the installed one is shown,
// so an element saying no build was taken means none was there.
func TestABuildAlreadyInstalledIsShownUnchanged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	logger := zaptest.NewLogger(t).Sugar()
	workspace := filepath.Join("_work", "pyre", "pyre")

	first, err := OpenRunner(aRunner(t))
	require.NoError(t, err)
	landBuild(t, filepath.Join(first.Path(), workspace), "pyre", "1.0.0", []byte("\x7fELF pyre"), "")
	_, err = first.Take(filepath.Join(first.Path(), workspace, buildArchive("pyre", "1.0.0")+".sha256"), logger)
	require.NoError(t, err)

	// The same build, seen by a runner watched afresh, as after the node restarts.
	again, err := OpenRunner(aRunner(t))
	require.NoError(t, err)
	landBuild(t, filepath.Join(again.Path(), workspace), "pyre", "1.0.0", []byte("\x7fELF pyre"), "")
	taken, err := again.Take(filepath.Join(again.Path(), workspace, buildArchive("pyre", "1.0.0")+".sha256"), logger)
	require.NoError(t, err)
	assert.False(t, taken.Changed)

	shown := again.Stats().Taken
	require.Len(t, shown, 1)
	assert.Equal(t, "pyre", shown[0].Plugin)
	assert.False(t, shown[0].Changed)
}

// What the GitHub element shows of a runner, read off its directory.
func TestARunnerSaysWhatItIs(t *testing.T) {
	runner, err := OpenRunner(aRunner(t))
	require.NoError(t, err)
	stats := runner.Stats()
	assert.Equal(t, "q-api-box", stats.Name)
	assert.Equal(t, "https://github.com/org", stats.GitHubURL)
}
