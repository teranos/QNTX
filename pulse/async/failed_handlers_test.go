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
// The handlers that failed most in a window, most first. A job a restart cut
// off is not the handler failing, and a failure before the window is not in it.
func TestTheHandlersThatFailedMostInAWindow(t *testing.T) {
	store := NewStore(qntxtest.CreateTestDB(t))
	now := time.Now()
	hourAgo := now.Add(-time.Hour)

	jobs := []*Job{
		{ID: "a1", HandlerName: "garden.water", Status: JobStatusFailed, Error: "account 1 refused", UpdatedAt: hourAgo},
		{ID: "a2", HandlerName: "garden.water", Status: JobStatusFailed, Error: "account 2 refused", UpdatedAt: hourAgo},
		{ID: "a3", HandlerName: "garden.water", Status: JobStatusFailed, Error: "account 3 refused", UpdatedAt: hourAgo.Add(time.Minute)},
		{ID: "i1", HandlerName: "garden.prune", Status: JobStatusFailed, Error: "inventory refused", UpdatedAt: hourAgo},
		{ID: "old", HandlerName: "garden.seed", Status: JobStatusFailed, Error: "last month", UpdatedAt: now.Add(-10 * 24 * time.Hour)},
		{ID: "cut", HandlerName: "garden.harvest", Status: JobStatusFailed, Error: OrphanedError, UpdatedAt: hourAgo},
		{ID: "ok", HandlerName: "garden.water", Status: JobStatusCompleted, UpdatedAt: hourAgo},
	}
	for _, j := range jobs {
		j.CreatedAt = j.UpdatedAt
		require.NoError(t, store.CreateJob(j))
	}

	failed, err := store.FailedHandlersSince(now.Add(-7*24*time.Hour), 3)
	require.NoError(t, err)
	require.Len(t, failed, 2)
	assert.Equal(t, HandlerFailures{Handler: "garden.water", Failures: 3, LastError: "account 3 refused"}, failed[0])
	assert.Equal(t, HandlerFailures{Handler: "garden.prune", Failures: 1, LastError: "inventory refused"}, failed[1])
}
