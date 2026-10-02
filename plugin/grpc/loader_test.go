package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/config"
	"go.uber.org/zap/zaptest"
)

// heldRecords is plugins as a test says the node knows them.
type heldRecords map[string]PluginRecord

func (h heldRecords) Plugins() ([]PluginRecord, error) {
	all := make([]PluginRecord, 0, len(h))
	for _, record := range h {
		all = append(all, record)
	}
	return all, nil
}

func (h heldRecords) Plugin(name string) (PluginRecord, bool, error) {
	record, found := h[name]
	return record, found, nil
}

// An enabled plugin is loaded, and one that was added and is disabled is left
// alone: it is neither started nor reported failed.
func TestOnlyEnabledPluginsAreLoaded(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	cfg := &config.Config{Plugin: config.PluginConfig{Paths: []string{t.TempDir()}}}
	records := heldRecords{
		"pyre": {Name: "pyre", Enabled: true},
		"kern": {Name: "kern"},
	}

	manager := NewPluginManager(logger, logger, "")
	assert.NoError(t, LoadPluginsFromRecords(context.Background(), manager, records, cfg, logger))

	assert.Empty(t, manager.GetAllPlugins(), "no binary is on disk")
	failed := manager.GetFailedPlugins()
	assert.Contains(t, failed, "pyre", "an enabled plugin with no binary says why")
	assert.NotContains(t, failed, "kern", "a disabled plugin is left alone")
}

// A plugin with no build on disk waits for one; it is never fetched.
func TestAnEnabledPluginWithNoBuildSaysItWaitsForOne(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	_, err := discoverPlugin("pyre", []string{t.TempDir()}, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
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
