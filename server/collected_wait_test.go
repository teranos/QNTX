package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appcfg "github.com/teranos/QNTX/internal/config"
)

// "「[Opus 5.5] and as a human i can dial it」"
// 「[Opus 5.5] collected.wait is every model's wait, and under a minute the node looks」
// 「[Opus 5.5] more often; saying nothing leaves each model's own.」
func TestCollectedWaitIsDialledInAmToml(t *testing.T) {
	fable, opus, sonnet, looked := collectedForFable, collectedForOpus, collectedForSonnet, collectedLooked
	t.Cleanup(func() {
		collectedForFable, collectedForOpus, collectedForSonnet, collectedLooked = fable, opus, sonnet, looked
	})

	cfg := &appcfg.Config{}
	require.NoError(t, dialCollected(cfg))
	assert.Equal(t, 6*time.Hour, waitOf("claude-fable-5-2"), "saying nothing changed a model's wait")

	cfg.Collected.Wait = "0s"
	require.NoError(t, dialCollected(cfg))
	for _, model := range []string{"claude-fable-5-2", "claude-opus-5-5", "claude-sonnet-5-5", ""} {
		assert.Zero(t, waitOf(model), model)
	}
	assert.Equal(t, time.Second, collectedLooked, "a wait of nothing is looked for every second")

	for _, said := range []string{"soon", "-1m"} {
		cfg.Collected.Wait = said
		assert.Error(t, dialCollected(cfg), said)
	}
}
