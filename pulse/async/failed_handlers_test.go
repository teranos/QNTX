package async

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// "top 3 handler failures"
//
// "I WANT TO SEE THE THREE MOST FREQUENTLY OCCURING ONES"
//
// The errors handlers failed with most in a window, most first. A job a
// restart cut off is not the handler failing, and a failure before the window
// is not in it.
func TestTheErrorsHandlersFailedWithMostInAWindow(t *testing.T) {
	store := NewStore(qntxtest.CreateTestDB(t))
	now := time.Now()
	hourAgo := now.Add(-time.Hour)

	jobs := []*Job{
		{Pulse: NoPulse(), Started: NotYet(), Completed: NotYet(), ID: "a1", HandlerName: "garden.water", Status: JobStatusFailed, Error: "account refused", UpdatedAt: hourAgo},
		{Pulse: NoPulse(), Started: NotYet(), Completed: NotYet(), ID: "a2", HandlerName: "garden.water", Status: JobStatusFailed, Error: "account refused", UpdatedAt: hourAgo},
		{Pulse: NoPulse(), Started: NotYet(), Completed: NotYet(), ID: "a3", HandlerName: "garden.water", Status: JobStatusFailed, Error: "account refused", UpdatedAt: hourAgo.Add(time.Minute)},
		{Pulse: NoPulse(), Started: NotYet(), Completed: NotYet(), ID: "a4", HandlerName: "garden.water", Status: JobStatusFailed, Error: "pump dry", UpdatedAt: hourAgo},
		{Pulse: NoPulse(), Started: NotYet(), Completed: NotYet(), ID: "i1", HandlerName: "garden.prune", Status: JobStatusFailed, Error: "inventory refused", UpdatedAt: hourAgo},
		{Pulse: NoPulse(), Started: NotYet(), Completed: NotYet(), ID: "i2", HandlerName: "garden.prune", Status: JobStatusFailed, Error: "inventory refused", UpdatedAt: hourAgo},
		{Pulse: NoPulse(), Started: NotYet(), Completed: NotYet(), ID: "old", HandlerName: "garden.seed", Status: JobStatusFailed, Error: "last month", UpdatedAt: now.Add(-10 * 24 * time.Hour)},
		{Pulse: NoPulse(), Started: NotYet(), Completed: NotYet(), ID: "cut", HandlerName: "garden.harvest", Status: JobStatusFailed, Error: OrphanedError, UpdatedAt: hourAgo},
		{Pulse: NoPulse(), Started: NotYet(), Completed: NotYet(), ID: "ok", HandlerName: "garden.water", Status: JobStatusCompleted, UpdatedAt: hourAgo},
	}
	for _, j := range jobs {
		j.CreatedAt = j.UpdatedAt
		require.NoError(t, store.CreateJob(j))
	}

	failed, err := store.FailedHandlersSince(now.Add(-7*24*time.Hour), 3)
	require.NoError(t, err)
	assert.Equal(t, []HandlerFailures{
		{Handler: "garden.water", Failures: 3, Error: "account refused"},
		{Handler: "garden.prune", Failures: 2, Error: "inventory refused"},
		{Handler: "garden.water", Failures: 1, Error: "pump dry"},
	}, failed)
}
