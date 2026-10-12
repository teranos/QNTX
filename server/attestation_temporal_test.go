package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/types"
)

// boundOf is where a side of a range is bounded, failing the test when it is
// not bounded.
func boundOf(t *testing.T, b types.TimeBound) time.Time {
	t.Helper()
	at, bounded, err := b.At("side")
	require.NoError(t, err)
	require.True(t, bounded, "the side is unbounded")
	return at
}

// The REST surface speaks the same temporal expressions as the ax language —
// an ISO date in since/until lands as the range boundary it names.
func TestSinceAndUntilBecomeTheRangeBoundaries(t *testing.T) {
	start, end, errMsg := parseTemporalParams("2025-01-01", "2025-02-01", "")

	require.Empty(t, errMsg)
	from, to := boundOf(t, start), boundOf(t, end)
	assert.Equal(t, time.Date(2025, 1, 1, 0, 0, 0, 0, from.Location()), from)
	assert.Equal(t, time.Date(2025, 2, 1, 0, 0, 0, 0, to.Location()), to)
}

// A boundary alone is a valid range: since with no until is open-ended, and
// says so.
func TestSinceAloneLeavesTheEndOpen(t *testing.T) {
	start, end, errMsg := parseTemporalParams("2025-01-01", "", "")

	require.Empty(t, errMsg)
	boundOf(t, start)
	assert.Equal(t, types.Unbounded(), end)
}

// on names a day, so it spans that whole day — midnight to midnight — the
// same way the ax grammar's on clause resolves.
func TestOnSpansTheFullDay(t *testing.T) {
	start, end, errMsg := parseTemporalParams("", "", "2025-01-15")

	require.Empty(t, errMsg)
	from, to := boundOf(t, start), boundOf(t, end)
	assert.Equal(t, time.Date(2025, 1, 15, 0, 0, 0, 0, from.Location()), from)
	assert.Equal(t, from.Add(24*time.Hour), to)
}

// A date that is not year first is refused rather than guessed at: 03/04/2025
// is April 3 to some and March 4 to others.
func TestSinceRefusesDatesThatAreNotYearFirst(t *testing.T) {
	for _, expr := range []string{"01/15/2025", "03/04/2025", "01-15-2025"} {
		_, _, errMsg := parseTemporalParams(expr, "", "")
		assert.NotEmpty(t, errMsg, expr)
	}
}

// Temporal is one clause in the grammar; on beside since or until is refused
// rather than silently picking one.
func TestOnRefusesToCombineWithSinceOrUntil(t *testing.T) {
	_, _, errMsg := parseTemporalParams("2025-01-01", "", "2025-01-15")
	assert.Contains(t, errMsg, "cannot combine")

	_, _, errMsg = parseTemporalParams("", "2025-02-01", "2025-01-15")
	assert.Contains(t, errMsg, "cannot combine")
}

// An expression the parser cannot read is a 400 naming the expression, not an
// unfiltered query that quietly returns everything.
func TestAnUnparseableExpressionIsRefusedByName(t *testing.T) {
	_, _, errMsg := parseTemporalParams("not-a-time", "", "")
	assert.Contains(t, errMsg, "not-a-time")

	_, _, errMsg = parseTemporalParams("", "also-not-a-time", "")
	assert.Contains(t, errMsg, "also-not-a-time")

	_, _, errMsg = parseTemporalParams("", "", "never")
	assert.Contains(t, errMsg, "never")
}

// No temporal params means no temporal filter: both sides unbounded, said.
func TestAbsentParamsLeaveTheRangeUnbounded(t *testing.T) {
	start, end, errMsg := parseTemporalParams("", "", "")

	assert.Empty(t, errMsg)
	assert.Equal(t, types.Unbounded(), start)
	assert.Equal(t, types.Unbounded(), end)
}
