//go:build qntxwasm

package parser

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// at sets timeNow for one test.
func at(t *testing.T, now time.Time) {
	t.Helper()
	was := timeNow
	timeNow = func() time.Time { return now }
	t.Cleanup(func() { timeNow = was })
}

// The resolver itself is tested in crates/ats/src/temporal.rs. These say Go
// reaches it, and that what Go and the ax language read is one reading.

func TestLastMonthIsACalendarMonth(t *testing.T) {
	at(t, time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC))

	got, err := ParseTemporalExpression("last month")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 2, 28, 10, 0, 0, 0, time.UTC), *got)
}

func TestDatesThatAreNotYearFirstAreRefused(t *testing.T) {
	for _, expr := range []string{"01/15/2025", "01-15-2025", "03/04/2025", "12/11/10"} {
		_, err := ParseTemporalExpression(expr)
		assert.Error(t, err, expr)
	}

	got, err := ParseTemporalExpression("2025-01-15 10:30:00")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC), *got)
}

func TestOnIsTheUTCCalendarDay(t *testing.T) {
	at(t, time.Date(2024, 6, 15, 14, 30, 0, 0, time.UTC))

	start, end, err := ParseTemporalDay("yesterday")
	require.NoError(t, err)
	assert.Equal(t, time.Date(2024, 6, 14, 0, 0, 0, 0, time.UTC), start)
	assert.Equal(t, time.Date(2024, 6, 15, 0, 0, 0, 0, time.UTC), end)
}

// The bug this closes: `since last month` read one way through ax and another
// through since=. Both now come from the same resolver.
func TestAxAndSinceReadLastMonthAlike(t *testing.T) {
	at(t, time.Date(2026, 3, 31, 10, 0, 0, 0, time.UTC))

	filter, err := ParseAxCommand([]string{"ALICE", "since", "last", "month"})
	require.NoError(t, err)
	fromAx, bounded, err := filter.TimeStart.At("since")
	require.NoError(t, err)
	require.True(t, bounded)

	fromSince, err := ParseTemporalExpression("last month")
	require.NoError(t, err)
	assert.True(t, fromAx.Equal(*fromSince), "ax %v, since %v", fromAx, *fromSince)

	axDay, err := ParseAxCommand([]string{"ALICE", "on", "yesterday"})
	require.NoError(t, err)
	axStart, _, err := axDay.TimeStart.At("on")
	require.NoError(t, err)
	start, _, err := ParseTemporalDay("yesterday")
	require.NoError(t, err)
	assert.True(t, axStart.Equal(start), "ax %v, on %v", axStart, start)
}

func TestUnreadableExpressionsAreRefused(t *testing.T) {
	for _, expr := range []string{"", "not-a-date", "2024-02-30", "3 fortnights ago"} {
		_, err := ParseTemporalExpression(expr)
		assert.Error(t, err, expr)
	}
}
