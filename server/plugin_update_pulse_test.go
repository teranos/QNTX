package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/plugin/grpc"
	"github.com/teranos/QNTX/pulse/schedule"
	"go.uber.org/zap"
)

// The production interval, so a test cannot pass at a cadence nothing runs at.
func updateInterval() int { return int(grpc.UpdatePollInterval.Seconds()) }

func nop() *zap.SugaredLogger { return zap.NewNop().Sugar() }

// ListJobsDue selects on `next_run_at <= now`, which no NULL satisfies, so a
// job created without a first run is never executed and never gets one.
func TestUpdateScheduleBecomesDue(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := schedule.NewStore(db)
	now := time.Now()

	ensureUpdateSchedule(store, updateInterval(), now, nop())

	all, err := store.ListAllScheduledJobs()
	require.NoError(t, err)
	require.Len(t, all, 1, "one plugin.update job")
	require.Equal(t, grpc.UpdateHandlerName, all[0].HandlerName)

	due, err := store.ListJobsDue(now.Add(grpc.UpdatePollInterval + time.Second))
	require.NoError(t, err)
	assert.Len(t, due, 1, "a job the scheduler will never execute is not a schedule")
}

// A job left active with the right interval and no next run. Only a restart
// can repair it: next_run_at is written after an execution it cannot reach.
func TestUpdateScheduleRepairsJobThatCanNeverRun(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := schedule.NewStore(db)
	now := time.Now()

	require.NoError(t, store.CreateJob(&schedule.Job{
		Id:              "SPJ_plugin_update_existing",
		HandlerName:     grpc.UpdateHandlerName,
		IntervalSeconds: int32(updateInterval()),
		State:           schedule.StateActive,
	}))

	ensureUpdateSchedule(store, updateInterval(), now, nop())

	all, err := store.ListAllScheduledJobs()
	require.NoError(t, err)
	assert.Len(t, all, 1, "the existing job is repaired, not duplicated")

	due, err := store.ListJobsDue(now.Add(grpc.UpdatePollInterval + time.Second))
	require.NoError(t, err)
	assert.Len(t, due, 1, "a job stuck with no next run is put back in the schedule")
}
