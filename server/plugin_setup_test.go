package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin"
	grpcplugin "github.com/teranos/QNTX/plugin/grpc"
	"go.uber.org/zap"
)

// A plugin's card says what still stands between it and its tools working,
// before Enable is pressed, read off its record.
//
// "you can now immediately see, you need to do some separate shit first"
func TestAPluginSaysWhatItStillNeeds(t *testing.T) {
	records := []grpcplugin.PluginRecord{
		// Added and nothing more: QNTX does not build it, so nothing stands.
		{Name: "handmade", Repo: "https://github.com/o/handmade", Config: map[string]string{}},
		// Names a source and no command: the build cannot run.
		{Name: "halfsaid", Repo: "https://github.com/o/halfsaid", Config: map[string]string{
			buildCore: "github.com/o/halfsaid@main", buildOutput: "bin/qntx-halfsaid-plugin"}},
		// Names its build whole, and no build has landed under this home.
		{Name: "unbuilt-setup-test", Repo: "https://github.com/o/unbuilt", Config: map[string]string{
			buildCore: "github.com/o/unbuilt@main", buildCommand: "go build -o bin/qntx-unbuilt-setup-test-plugin .", buildOutput: "bin/qntx-unbuilt-setup-test-plugin"}},
	}
	h := NewPluginHandler(plugin.NewRegistry("test", zap.NewNop().Sugar()), zap.NewNop().Sugar(),
		func() pluginProbe { return probed{results: map[string]plugin.HealthStatus{}, at: time.Now()} },
		noSigils, func() ([]grpcplugin.PluginRecord, error) { return records, nil })

	listed := h.list()
	holds(t, bareNode().pluginsSignum(), "list", listed)
	setup := map[string][]string{}
	for _, info := range listed.GetPlugins() {
		setup[info.GetName()] = info.GetSetup()
	}
	require.Len(t, setup, 3)
	assert.Empty(t, setup["handmade"], "a plugin QNTX does not build was told it needs something")
	require.Len(t, setup["halfsaid"], 1)
	assert.Contains(t, setup["halfsaid"][0], buildCommand)
	require.Len(t, setup["unbuilt-setup-test"], 1)
	assert.Contains(t, setup["unbuilt-setup-test"][0], "no build of unbuilt-setup-test is installed yet")
	assert.Contains(t, setup["unbuilt-setup-test"][0], "github.com/o/unbuilt@main")
}
