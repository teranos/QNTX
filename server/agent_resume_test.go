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

	none, wasCutOff := cutOff([]*types.As{})
	assert.False(t, wasCutOff, "no turn, nothing cut off")
	assert.Empty(t, none)
	ended, wasCutOff := cutOff([]*types.As{first, firstEnded})
	assert.False(t, wasCutOff, "the last turn ended")
	assert.Equal(t, "said-1", ended.ID)

	killed, wasCutOff := cutOff([]*types.As{second, firstEnded, first})
	require.True(t, wasCutOff, "a turn with no end was cut off, read in time order")
	assert.Equal(t, "said-2", killed.ID)

	stopped := sessionRow("fail-2", "StopFailure", at.Add(3*time.Minute), map[string]any{"error": nodeStopped})
	byStopping, wasCutOff := cutOff([]*types.As{first, firstEnded, second, stopped})
	require.True(t, wasCutOff, "a turn the node's stopping ended was cut off")
	assert.Equal(t, "said-2", byStopping.ID)

	failed := sessionRow("fail-2", "StopFailure", at.Add(3*time.Minute), map[string]any{"error": "error_during_execution"})
	failedTurn, wasCutOff := cutOff([]*types.As{first, firstEnded, second, failed})
	assert.False(t, wasCutOff, "a turn that failed on its own is not picked up")
	assert.Equal(t, "said-2", failedTurn.ID)
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
