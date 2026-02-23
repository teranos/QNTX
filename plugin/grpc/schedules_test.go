package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/schedule"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"go.uber.org/zap/zaptest"
)

func TestSetupPluginSchedules(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zaptest.NewLogger(t).Sugar()

	schedules := []*protocol.ScheduleInfo{
		{
			HandlerName:      "test.handler",
			IntervalSeconds:  3600,
			EnabledByDefault: true,
			Description:      "Test handler description",
			AtsCode:          "ats{test.handler}",
		},
	}

	err := SetupPluginSchedules(db, "testplugin", schedules, logger)
	require.NoError(t, err)

	// Verify schedule was created
	store := schedule.NewStore(db)
	jobs, err := store.ListAllScheduledJobs()
	require.NoError(t, err)
	require.Len(t, jobs, 1)

	job := jobs[0]
	assert.Equal(t, "test.handler", job.HandlerName)
	assert.Equal(t, 3600, job.IntervalSeconds)
	assert.Equal(t, schedule.StateActive, job.State)
	assert.Equal(t, "ats{test.handler}", job.ATSCode)
	assert.Contains(t, job.Metadata, "testplugin")
}

func TestSetupPluginSchedules_DisabledByDefault(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zaptest.NewLogger(t).Sugar()

	schedules := []*protocol.ScheduleInfo{
		{
			HandlerName:      "test.handler",
			IntervalSeconds:  0,
			EnabledByDefault: false,
			Description:      "Disabled handler",
		},
	}

	err := SetupPluginSchedules(db, "testplugin", schedules, logger)
	require.NoError(t, err)

	// Verify schedule was NOT created (disabled)
	store := schedule.NewStore(db)
	jobs, err := store.ListAllScheduledJobs()
	require.NoError(t, err)
	assert.Len(t, jobs, 0)
}

func TestSetupPluginSchedules_Idempotent(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zaptest.NewLogger(t).Sugar()

	schedules := []*protocol.ScheduleInfo{
		{
			HandlerName:      "test.handler",
			IntervalSeconds:  3600,
			EnabledByDefault: true,
			Description:      "Test handler",
		},
	}

	// First call - creates schedule
	err := SetupPluginSchedules(db, "testplugin", schedules, logger)
	require.NoError(t, err)

	store := schedule.NewStore(db)
	jobs, err := store.ListAllScheduledJobs()
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	firstJobID := jobs[0].ID

	// Second call - should not duplicate
	err = SetupPluginSchedules(db, "testplugin", schedules, logger)
	require.NoError(t, err)

	jobs, err = store.ListAllScheduledJobs()
	require.NoError(t, err)
	assert.Len(t, jobs, 1)
	assert.Equal(t, firstJobID, jobs[0].ID)
}

func TestSetupPluginSchedules_UpdateInterval(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zaptest.NewLogger(t).Sugar()

	// Create schedule with interval 3600
	schedules := []*protocol.ScheduleInfo{
		{
			HandlerName:      "test.handler",
			IntervalSeconds:  3600,
			EnabledByDefault: true,
			Description:      "Test handler",
		},
	}

	err := SetupPluginSchedules(db, "testplugin", schedules, logger)
	require.NoError(t, err)

	store := schedule.NewStore(db)
	jobs, err := store.ListAllScheduledJobs()
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, 3600, jobs[0].IntervalSeconds)

	// Update to interval 7200
	schedules[0].IntervalSeconds = 7200
	err = SetupPluginSchedules(db, "testplugin", schedules, logger)
	require.NoError(t, err)

	jobs, err = store.ListAllScheduledJobs()
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, 7200, jobs[0].IntervalSeconds)
}

func TestSetupPluginSchedules_MultipleSchedules(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zaptest.NewLogger(t).Sugar()

	schedules := []*protocol.ScheduleInfo{
		{
			HandlerName:      "test.handler1",
			IntervalSeconds:  3600,
			EnabledByDefault: true,
			Description:      "Handler 1",
		},
		{
			HandlerName:      "test.handler2",
			IntervalSeconds:  7200,
			EnabledByDefault: false,
			Description:      "Handler 2",
		},
	}

	err := SetupPluginSchedules(db, "testplugin", schedules, logger)
	require.NoError(t, err)

	store := schedule.NewStore(db)
	jobs, err := store.ListAllScheduledJobs()
	require.NoError(t, err)
	require.Len(t, jobs, 2)

	// Find jobs by handler name
	var job1, job2 *schedule.Job
	for i := range jobs {
		if jobs[i].HandlerName == "test.handler1" {
			job1 = jobs[i]
		} else if jobs[i].HandlerName == "test.handler2" {
			job2 = jobs[i]
		}
	}

	require.NotNil(t, job1)
	require.NotNil(t, job2)
	assert.Equal(t, schedule.StateActive, job1.State)
	assert.Equal(t, schedule.StatePaused, job2.State)
}
