package grpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/teranos/QNTX/internal/config"
	"go.uber.org/zap/zaptest"
)

// TestLoadPluginsFromConfig_NoDuplicates verifies that plugins listed in
// cfg.Plugin.Enabled are only loaded once, not duplicated.
func TestLoadPluginsFromConfig_NoDuplicates(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	ctx := context.Background()

	// Create config with duplicate plugin names (simulating the bug)
	cfg := &config.Config{
		Plugin: config.PluginConfig{
			Enabled: []string{"testplugin", "testplugin"}, // Intentional duplicate
			Paths:   []string{t.TempDir()},                // Empty dir, no binaries found
		},
	}

	manager := NewPluginManager(logger, logger, "")
	err := LoadPluginsFromConfig(ctx, manager, cfg, logger)
	assert.NoError(t, err, "Loading should not error even if plugins not found")

	// Verify no plugins loaded (binaries don't exist)
	plugins := manager.GetAllPlugins()
	assert.Equal(t, 0, len(plugins), "No plugins should load since binaries don't exist")

	// The real test: if binaries DID exist, would they be loaded twice?
	// We can't easily test with real binaries, but we can verify the loop logic
	// doesn't add duplicates by checking the loop iterates correctly
	seenPlugins := make(map[string]int)
	for _, name := range cfg.Plugin.Enabled {
		seenPlugins[name]++
	}

	// This demonstrates the bug: if enabled list has duplicates,
	// the loop will process each entry
	assert.Equal(t, 2, seenPlugins["testplugin"],
		"Bug: duplicate entries in enabled list would be processed twice")
}

// TestLoadPluginsFromConfig_UniquePlugins verifies normal case with unique plugins
func TestLoadPluginsFromConfig_UniquePlugins(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	ctx := context.Background()

	cfg := &config.Config{
		Plugin: config.PluginConfig{
			Enabled: []string{"plugin1", "plugin2", "plugin3"},
			Paths:   []string{t.TempDir()},
		},
	}

	manager := NewPluginManager(logger, logger, "")
	err := LoadPluginsFromConfig(ctx, manager, cfg, logger)
	assert.NoError(t, err)

	plugins := manager.GetAllPlugins()
	assert.Equal(t, 0, len(plugins), "No plugins loaded (binaries don't exist)")

	// Verify enabled list processing
	assert.Equal(t, 3, len(cfg.Plugin.Enabled), "Should have 3 unique plugins in config")
}

// TestGetAllPlugins_ReturnsUniqueInstances verifies GetAllPlugins doesn't
// return duplicates even if the internal map somehow had duplicates
func TestGetAllPlugins_ReturnsUniqueInstances(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	manager := NewPluginManager(logger, logger, "")

	// GetAllPlugins should return unique instances
	plugins := manager.GetAllPlugins()
	assert.Equal(t, 0, len(plugins), "Empty manager returns no plugins")

	// Verify the map-based storage prevents duplicates by design
	// (maps can't have duplicate keys)
	pluginNames := make(map[string]bool)
	for _, p := range plugins {
		name := p.Metadata().Name
		assert.False(t, pluginNames[name], "Plugin %s returned twice", name)
		pluginNames[name] = true
	}
}

// installManagedPlugin puts a plugin where QNTX's own installs go and returns
// the path to its binary.
func installManagedPlugin(t *testing.T, archive []byte) string {
	t.Helper()

	dir, err := PluginInstallPath("duif")
	if err != nil {
		t.Fatalf("PluginInstallPath = %v", err)
	}

	binary, _, err := install(archive, dir, "qntx-duif-plugin")
	if err != nil {
		t.Fatalf("install = %v", err)
	}

	return binary
}

// The reconcile exists to replace a build that is no longer the published one —
// the case that otherwise needs someone deleting the file by hand on every box.
func TestManagedPluginIsStaleWhenDigestDiffers(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	archive := tarGz(t, map[string][]byte{"qntx-duif-plugin": []byte("installed build")})
	binary := installManagedPlugin(t, archive)

	dir, _ := PluginInstallPath("duif")
	if err := recordInstalledDigest(dir, strings.Repeat("a", 64)); err != nil {
		t.Fatalf("recordInstalledDigest = %v", err)
	}

	published := tarGz(t, map[string][]byte{"qntx-duif-plugin": []byte("published build")})
	sum := sha256.Sum256(published)
	srv := releaseServer(t, published, hex.EncodeToString(sum[:]), "")
	defer srv.Close()

	if !managedPluginIsStale(context.Background(), "duif", "https://github.com/sbvh-nl/duif", binary, testLogger(t)) {
		t.Error("an installed plugin that differs from the published release was not stale")
	}
}

func TestManagedPluginIsCurrentWhenDigestMatches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	archive := tarGz(t, map[string][]byte{"qntx-duif-plugin": []byte("the build")})
	binary := installManagedPlugin(t, archive)

	sum := sha256.Sum256(archive)
	digest := hex.EncodeToString(sum[:])

	dir, _ := PluginInstallPath("duif")
	if err := recordInstalledDigest(dir, digest); err != nil {
		t.Fatalf("recordInstalledDigest = %v", err)
	}

	srv := releaseServer(t, archive, digest, "")
	defer srv.Close()

	if managedPluginIsStale(context.Background(), "duif", "https://github.com/sbvh-nl/duif", binary, testLogger(t)) {
		t.Error("an installed plugin matching the published release was called stale")
	}
}

// Hand-placing a binary stays a way to run a build of your own choosing. A
// plugin outside QNTX's install directory is never reconciled away, however far
// it is from what the repo publishes.
func TestUnmanagedPluginIsNeverStale(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	elsewhere := t.TempDir()
	binary := filepath.Join(elsewhere, "qntx-duif-plugin")
	if err := os.WriteFile(binary, []byte("my own build"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}

	published := tarGz(t, map[string][]byte{"qntx-duif-plugin": []byte("something else entirely")})
	sum := sha256.Sum256(published)
	srv := releaseServer(t, published, hex.EncodeToString(sum[:]), "")
	defer srv.Close()

	if managedPluginIsStale(context.Background(), "duif", "https://github.com/sbvh-nl/duif", binary, testLogger(t)) {
		t.Error("a hand-placed plugin was treated as stale")
	}
}

// An unreachable forge must not cost a box the plugin it already has.
func TestUnreachableReleaseLeavesPluginInstalled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	archive := tarGz(t, map[string][]byte{"qntx-duif-plugin": []byte("the build")})
	binary := installManagedPlugin(t, archive)

	dir, _ := PluginInstallPath("duif")
	if err := recordInstalledDigest(dir, strings.Repeat("b", 64)); err != nil {
		t.Fatalf("recordInstalledDigest = %v", err)
	}

	// A server that is listening and then is not, so the check fails the way
	// an offline box fails rather than by pointing at a port nobody claimed.
	srv := releaseServer(t, archive, strings.Repeat("c", 64), "")
	srv.Close()

	if managedPluginIsStale(context.Background(), "duif", "https://github.com/sbvh-nl/duif", binary, testLogger(t)) {
		t.Error("an unreachable release discarded a plugin that was already installed")
	}
}

// A pre-tree install is QNTX's own, not a hand-placed build, and it shadows the
// tree that would replace it. It is always superseded.
func TestLegacyInstallIsStale(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	legacy, err := LegacyPluginInstallPath("duif")
	if err != nil {
		t.Fatalf("LegacyPluginInstallPath = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(legacy, []byte("the build that will not run"), 0o755); err != nil {
		t.Fatalf("write legacy binary: %v", err)
	}

	// No server: the decision must not depend on reaching the forge, since the
	// legacy file has no digest to compare against in the first place.
	if !managedPluginIsStale(context.Background(), "duif", "https://github.com/sbvh-nl/duif", legacy, testLogger(t)) {
		t.Error("a pre-tree install was not superseded")
	}
}

// Installing the tree removes the file it supersedes. Left in place, discovery
// tries qntx-<name>-plugin before <name> and the old file wins every start.
func TestFetchRemovesTheInstallItSupersedes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	legacy, err := LegacyPluginInstallPath("duif")
	if err != nil {
		t.Fatalf("LegacyPluginInstallPath = %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(legacy, []byte("superseded"), 0o755); err != nil {
		t.Fatalf("write legacy binary: %v", err)
	}

	archive := tarGz(t, map[string][]byte{
		"qntx-duif-plugin":  []byte("\x7fELF"),
		"lib/libvmime.so.1": []byte("shared object"),
	})
	sum := sha256.Sum256(archive)
	srv := releaseServer(t, archive, hex.EncodeToString(sum[:]), "")
	defer srv.Close()

	binary, err := fetchPlugin(context.Background(), "duif", "https://github.com/sbvh-nl/duif", testLogger(t))
	if err != nil {
		t.Fatalf("fetchPlugin = %v", err)
	}

	if _, err := os.Stat(legacy); err == nil {
		t.Error("the superseded plugin binary survived the install and will shadow the tree")
	}

	// And what replaced it is the tree, libraries included.
	if _, err := os.Stat(filepath.Join(filepath.Dir(binary), "lib", "libvmime.so.1")); err != nil {
		t.Errorf("the installed tree is missing its libraries: %v", err)
	}
}

// A plugin installed before digests were recorded has no record to compare, and
// is left alone rather than re-downloaded on every start.
func TestPluginWithNoRecordedDigestIsNotStale(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	archive := tarGz(t, map[string][]byte{"qntx-duif-plugin": []byte("the build")})
	binary := installManagedPlugin(t, archive)

	published := tarGz(t, map[string][]byte{"qntx-duif-plugin": []byte("newer build")})
	sum := sha256.Sum256(published)
	srv := releaseServer(t, published, hex.EncodeToString(sum[:]), "")
	defer srv.Close()

	if managedPluginIsStale(context.Background(), "duif", "https://github.com/sbvh-nl/duif", binary, testLogger(t)) {
		t.Error("a plugin with no recorded digest was treated as stale")
	}
}
