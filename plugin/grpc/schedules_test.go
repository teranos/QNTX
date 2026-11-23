package grpc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/pulse/async"
	"github.com/teranos/QNTX/pulse/schedule"
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
	assert.Equal(t, "testplugin/test.handler", job.HandlerName)
	assert.Equal(t, int32(3600), job.IntervalSeconds)
	assert.Equal(t, schedule.StateActive, job.State)
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

	// Made, and held paused: it is not enabled by default
	store := schedule.NewStore(db)
	jobs, err := store.ListAllScheduledJobs()
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, schedule.StatePaused, jobs[0].State)
}

// "zero means zero"

// An interval of 0 is no ticking: the schedule is made as declared, with no
// interval, and the ticker never runs it.
func TestSetupPluginSchedules_ZeroIntervalNeverTicks(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zaptest.NewLogger(t).Sugar()

	err := SetupPluginSchedules(db, "testplugin", []*protocol.ScheduleInfo{{
		HandlerName:      "test.handler",
		IntervalSeconds:  0,
		EnabledByDefault: true,
	}}, logger)
	require.NoError(t, err)

	jobs, err := schedule.NewStore(db).ListAllScheduledJobs()
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, int32(0), jobs[0].IntervalSeconds, "a schedule with interval 0 was given one")
}

// An interval of fewer than no seconds is refused.
func TestSetupPluginSchedules_NegativeIntervalIsRefused(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zaptest.NewLogger(t).Sugar()

	err := SetupPluginSchedules(db, "testplugin", []*protocol.ScheduleInfo{{
		HandlerName:      "test.handler",
		IntervalSeconds:  -60,
		EnabledByDefault: true,
	}}, logger)
	require.Error(t, err)

	jobs, err := schedule.NewStore(db).ListAllScheduledJobs()
	require.NoError(t, err)
	assert.Empty(t, jobs)
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
	firstJobID := jobs[0].Id

	// Second call - should not duplicate
	err = SetupPluginSchedules(db, "testplugin", schedules, logger)
	require.NoError(t, err)

	jobs, err = store.ListAllScheduledJobs()
	require.NoError(t, err)
	assert.Len(t, jobs, 1)
	assert.Equal(t, firstJobID, jobs[0].Id)
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
	assert.Equal(t, int32(3600), jobs[0].IntervalSeconds)

	// Update to interval 7200
	schedules[0].IntervalSeconds = 7200
	err = SetupPluginSchedules(db, "testplugin", schedules, logger)
	require.NoError(t, err)

	jobs, err = store.ListAllScheduledJobs()
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	assert.Equal(t, int32(7200), jobs[0].IntervalSeconds)
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
		if jobs[i].HandlerName == "testplugin/test.handler1" {
			job1 = jobs[i]
		} else if jobs[i].HandlerName == "testplugin/test.handler2" {
			job2 = jobs[i]
		}
	}

	require.NotNil(t, job1)
	require.NotNil(t, job2)
	assert.Equal(t, schedule.StateActive, job1.State)
	assert.Equal(t, schedule.StatePaused, job2.State)
}

// 「[Sonnet 5.5, Fable 5.2] A plugin switched off has its schedules paused, and switched on again they」
// 「[Sonnet 5.5, Fable 5.2] run: what was paused by declaration stays paused.」
func TestPluginSchedulesFollowThePlugin(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zaptest.NewLogger(t).Sugar()
	declared := []*protocol.ScheduleInfo{
		{HandlerName: "poll", IntervalSeconds: 120, EnabledByDefault: true, Description: "polls"},
		{HandlerName: "held", IntervalSeconds: 120, EnabledByDefault: false, Description: "declared paused"},
	}
	require.NoError(t, SetupPluginSchedules(db, "duif", declared, logger))
	other := []*protocol.ScheduleInfo{{HandlerName: "poll", IntervalSeconds: 60, EnabledByDefault: true}}
	require.NoError(t, SetupPluginSchedules(db, "other", other, logger))

	states := func() map[string]string {
		jobs, err := schedule.NewStore(db).ListAllScheduledJobs()
		require.NoError(t, err)
		got := map[string]string{}
		for _, j := range jobs {
			got[j.HandlerName] = j.State
		}
		return got
	}

	// 「[Sonnet 5.5, Fable 5.2] Metadata that is no JSON object keeps no plugin running.」
	for id, metadata := range map[string]string{"AS-null": "null", "AS-text": "polls the pond"} {
		_, err := db.Exec(`UPDATE scheduled_pulse_jobs SET metadata = ? WHERE handler_name = 'duif/poll'`, metadata)
		require.NoError(t, err, id)
		require.NoError(t, PausePluginSchedules(db, "duif", logger), id)
		require.NoError(t, SetupPluginSchedules(db, "duif", declared, logger), id)
	}

	require.NoError(t, PausePluginSchedules(db, "duif", logger))
	assert.Equal(t, map[string]string{
		"duif/poll":  schedule.StateInactive,
		"duif/held":  schedule.StatePaused,
		"other/poll": schedule.StateActive,
	}, states())

	// 「[Sonnet 5.5, Fable 5.2] Back: the one the switch-off took out runs; the one declared paused does not.」
	require.NoError(t, SetupPluginSchedules(db, "duif", declared, logger))
	assert.Equal(t, map[string]string{
		"duif/poll":  schedule.StateActive,
		"duif/held":  schedule.StatePaused,
		"other/poll": schedule.StateActive,
	}, states())

	// 「[Sonnet 5.5, Fable 5.2] Paused by hand while the plugin is off, it is paused when the plugin is back.」
	require.NoError(t, PausePluginSchedules(db, "duif", logger))
	jobs, err := schedule.NewStore(db).ListAllScheduledJobs()
	require.NoError(t, err)
	for _, j := range jobs {
		if j.HandlerName == "duif/poll" {
			require.NoError(t, schedule.NewStore(db).UpdateJobState(j.Id, schedule.StatePaused))
		}
	}
	require.NoError(t, SetupPluginSchedules(db, "duif", declared, logger))
	assert.Equal(t, schedule.StatePaused, states()["duif/poll"], "enabling the plugin woke what a person paused")
}

// 「[Sonnet 5.5, Fable 5.2] A plugin taken off the node has no schedules left, and is added again with none.」
func TestDeletePluginSchedules(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	logger := zaptest.NewLogger(t).Sugar()
	declared := []*protocol.ScheduleInfo{{HandlerName: "poll", IntervalSeconds: 120, EnabledByDefault: true}}
	require.NoError(t, SetupPluginSchedules(db, "duif", declared, logger))
	require.NoError(t, SetupPluginSchedules(db, "other", declared, logger))

	// 「[Sonnet 5.5, Fable 5.2] A schedule another caller made, whose metadata names duif and whose」
	// 「[Sonnet 5.5, Fable 5.2] handler is not duif's: not duif's to pause or delete, whatever it says.」
	require.NoError(t, schedule.NewStore(db).CreateJob(&schedule.Job{
		MadeBy: string(async.MadeByNoCaller), Id: "AS-someone-elses", HandlerName: "report", IntervalSeconds: 60,
		NextRunAt: time.Now().Format(time.RFC3339), State: schedule.StateActive, Metadata: `{"plugin":"duif"}`,
		CreatedAt: time.Now().Format(time.RFC3339), UpdatedAt: time.Now().Format(time.RFC3339),
	}))

	require.NoError(t, PausePluginSchedules(db, "duif", logger))
	require.NoError(t, DeletePluginSchedules(db, "duif", logger))

	jobs, err := schedule.NewStore(db).ListAllScheduledJobs()
	require.NoError(t, err)
	listed := map[string]string{}
	for _, j := range jobs {
		listed[j.HandlerName] = j.State
	}
	assert.Equal(t, map[string]string{"other/poll": schedule.StateActive, "report": schedule.StateActive}, listed,
		"duif's are deleted, and nobody else's is touched")
}
