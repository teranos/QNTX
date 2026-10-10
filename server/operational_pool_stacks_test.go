package server

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A stall the pool only counts is one nobody can act on once it has passed:
// who waits for a connection is named while they wait.
func TestAStalledPoolNamesWhoWaitsForIt(t *testing.T) {
	_, db := createTestStore(t)
	db.SetMaxOpenConns(1)
	held, err := db.Conn(context.Background())
	require.NoError(t, err)

	waiting := make(chan error, 1)
	go func() {
		waiting <- db.PingContext(context.Background())
	}()
	require.Eventually(t, func() bool { return db.Stats().WaitCount > 0 }, 10e9, 1e6, "nothing waited for the one connection")

	stacks := inThePool()
	require.NoError(t, held.Close())
	require.NoError(t, <-waiting)

	named := strings.Join(stacks, "\n")
	assert.Contains(t, named, "TestAStalledPoolNamesWhoWaitsForIt", "the goroutine waiting for the connection is not named")
	assert.Contains(t, named, "database/sql.(*DB).conn")
}

// The same stack is said once, with how many goroutines stand on it.
func TestOneStackIsSaidOnceWithItsCount(t *testing.T) {
	dump := "goroutine 7 [select]:\ndatabase/sql.(*DB).conn(...)\nmain.ask()\n\n" +
		"goroutine 9 [select]:\ndatabase/sql.(*DB).conn(...)\nmain.ask()\n\n" +
		"goroutine 3 [running]:\nmain.other()\n"
	assert.Equal(t, []string{"2 goroutines [select]\ndatabase/sql.(*DB).conn(...)\nmain.ask()"}, poolStacks(dump))
}
