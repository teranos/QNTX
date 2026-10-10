package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/plugin"
	"github.com/teranos/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// unreadableRecords is a store that does not answer.
type unreadableRecords struct{}

func (unreadableRecords) Plugins() ([]PluginRecord, error) {
	return nil, errors.New("the system store did not answer")
}

func (unreadableRecords) Plugin(name string) (PluginRecord, bool, error) {
	return PluginRecord{}, false, errors.Newf("the system store did not answer for %s", name)
}

// A record that could not be read is an error, never a plugin with no record.
func TestAnUnreadableRecordIsAnError(t *testing.T) {
	SetPluginRecords(unreadableRecords{})
	t.Cleanup(func() { SetPluginRecords(recordsNotHanded{}) })

	_, _, err := pluginRecord("pyre")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pyre")
}

// Launch args that are not a JSON list stop the plugin from loading, and say why.
func TestMalformedArgsAreAnError(t *testing.T) {
	logger := zaptest.NewLogger(t).Sugar()
	cfg := &config.Config{Plugin: config.PluginConfig{Paths: []string{t.TempDir()}}}
	records := heldRecords{"pyre": {Name: "pyre", Enabled: true, Config: map[string]string{"args": "--name pyre"}}}

	manager := NewPluginManager(logger, logger, "")
	require.NoError(t, LoadPluginsFromRecords(context.Background(), manager, records, cfg, logger))

	failed := manager.GetFailedPlugins()
	require.Contains(t, failed, "pyre")
	assert.Contains(t, failed["pyre"], "args")
}

// A config value that does not read as the type asked for is said, not turned
// into zero silently: it is the config's error, which fails the Initialize.
func TestAConfigValueOfTheWrongTypeIsSaid(t *testing.T) {
	SetPluginRecords(heldRecords{"pyre": {Name: "pyre", Config: map[string]string{"poll_interval": "often"}}})
	t.Cleanup(func() { SetPluginRecords(recordsNotHanded{}) })

	config := NewConfigProvider(nil, nil, zap.NewNop().Sugar()).GetPluginConfig("pyre")
	assert.Equal(t, 0, config.GetInt("poll_interval"))
	err := config.(interface{ Err() error }).Err()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "poll_interval")
}

// A plugin whose record could not be read is not started with no config: its
// Initialize fails, and that failure is what Enable answers with.
func TestAPluginWhoseRecordIsUnreadableDoesNotInitialize(t *testing.T) {
	SetPluginRecords(unreadableRecords{})
	t.Cleanup(func() { SetPluginRecords(recordsNotHanded{}) })
	logger := zaptest.NewLogger(t).Sugar()
	services := plugin.NewServiceRegistry(plugin.NewRegistry("test", logger), nil, logger, nil, NewConfigProvider(nil, nil, logger), nil)

	proxy := &ExternalDomainProxy{metadata: plugin.Metadata{Name: "pyre"}, logger: logger}
	err := proxy.doInitialize(context.Background(), services)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pyre")
}

// A record that could not be read while handing a plugin its config is said,
// as the config's error.
func TestAnUnreadableRecordIsSaidWhenConfigIsAsked(t *testing.T) {
	SetPluginRecords(unreadableRecords{})
	t.Cleanup(func() { SetPluginRecords(recordsNotHanded{}) })

	config := NewConfigProvider(nil, nil, zap.NewNop().Sugar()).GetPluginConfig("pyre")
	assert.Empty(t, config.GetKeys())
	err := config.(interface{ Err() error }).Err()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pyre")
}

// A plugin with no record has nothing that says its config, and is not handed one.
func TestAPluginWithNoRecordIsNotHandedAConfig(t *testing.T) {
	SetPluginRecords(heldRecords{})
	t.Cleanup(func() { SetPluginRecords(nil) })

	config := NewConfigProvider(nil, nil, zap.NewNop().Sugar()).GetPluginConfig("pyre")
	assert.Empty(t, config.GetKeys())
	err := config.(interface{ Err() error }).Err()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no record")
}
