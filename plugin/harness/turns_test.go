package harness

import (
	"context"
	"strconv"
	"testing"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 「[Fable 5.2, Fable 5.1] The plugin keeps a Task while its turn runs, and the newest few after: core」
// 「[Fable 5.2, Fable 5.1] writes every turn down and asks for none again.」
func TestThePluginLetsGoOfTurnsThatEnded(t *testing.T) {
	ctx := context.Background()
	held := newTurns(2)

	running := &a2asdk.Task{ID: "running", ContextID: "c", Status: a2asdk.TaskStatus{State: a2asdk.TaskStateWorking}}
	_, err := held.Create(ctx, running)
	require.NoError(t, err)
	_, err = held.Create(ctx, running)
	assert.ErrorIs(t, err, taskstore.ErrTaskAlreadyExists)

	for turn := 0; turn < 5; turn++ {
		task := &a2asdk.Task{ID: a2asdk.TaskID("t-" + strconv.Itoa(turn)), ContextID: "c", Status: a2asdk.TaskStatus{State: a2asdk.TaskStateSubmitted}}
		version, err := held.Create(ctx, task)
		require.NoError(t, err)
		ended := *task
		ended.Status = a2asdk.TaskStatus{State: a2asdk.TaskStateCompleted}
		_, err = held.Update(ctx, &taskstore.UpdateRequest{Task: &ended, PrevVersion: version + 7})
		require.ErrorIs(t, err, taskstore.ErrConcurrentModification)
		_, err = held.Update(ctx, &taskstore.UpdateRequest{Task: &ended, PrevVersion: version})
		require.NoError(t, err)
	}

	for _, gone := range []a2asdk.TaskID{"t-0", "t-1", "t-2"} {
		_, err := held.Get(ctx, gone)
		assert.ErrorIs(t, err, a2asdk.ErrTaskNotFound, string(gone))
	}
	kept, err := held.Get(ctx, "t-4")
	require.NoError(t, err)
	assert.Equal(t, a2asdk.TaskStateCompleted, kept.Task.Status.State)
	still, err := held.Get(ctx, "running")
	require.NoError(t, err, "a Task whose turn still runs was let go of")
	assert.Equal(t, a2asdk.TaskStateWorking, still.Task.Status.State)
	assert.Len(t, held.held, 3)
}
