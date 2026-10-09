package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/types"
)

func sessionRow(id, predicate string, at time.Time, attrs map[string]any) *types.As {
	return &types.As{ID: id, Predicates: []string{predicate}, Timestamp: at, Attributes: attrs}
}

// A turn is cut off when it was said and nothing ended it, or the node
// stopping did; a turn that ended any other way is not picked up.
func TestACutOffTurnIsTheLastOneSaidAndNotEnded(t *testing.T) {
	at := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	first := sessionRow("said-1", "UserPromptSubmit", at, map[string]any{"prompt": "one"})
	firstEnded := sessionRow("stop-1", "Stop", at.Add(time.Minute), map[string]any{})
	second := sessionRow("said-2", "UserPromptSubmit", at.Add(2*time.Minute), map[string]any{"prompt": "two"})

	assert.Nil(t, cutOff(nil), "no turn, nothing cut off")
	assert.Nil(t, cutOff([]*types.As{first, firstEnded}), "the last turn ended")

	killed := cutOff([]*types.As{second, firstEnded, first})
	require.NotNil(t, killed, "a turn with no end was cut off, read in time order")
	assert.Equal(t, "said-2", killed.ID)

	stopped := sessionRow("fail-2", "StopFailure", at.Add(3*time.Minute), map[string]any{"error": nodeStopped})
	byStopping := cutOff([]*types.As{first, firstEnded, second, stopped})
	require.NotNil(t, byStopping, "a turn the node's stopping ended was cut off")
	assert.Equal(t, "said-2", byStopping.ID)

	failed := sessionRow("fail-2", "StopFailure", at.Add(3*time.Minute), map[string]any{"error": "error_during_execution"})
	assert.Nil(t, cutOff([]*types.As{first, firstEnded, second, failed}), "a turn that failed on its own is not picked up")
}

func TestWhatThePickedUpTurnIsTold(t *testing.T) {
	at := time.Date(2026, 10, 10, 9, 0, 0, 0, time.UTC)
	says, err := resumeSays(sessionRow("said-1", "UserPromptSubmit", at, map[string]any{"prompt": "check the deploy"}))
	require.NoError(t, err)
	assert.Contains(t, says, "check the deploy")
	assert.Contains(t, says, "2026-10-10T09:00:00Z")

	nothing, err := resumeSays(sessionRow("said-2", "UserPromptSubmit", at, map[string]any{}))
	assert.Error(t, err)
	assert.Empty(t, nothing)
}
