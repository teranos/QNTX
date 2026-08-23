package server

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The REST surface speaks the same temporal expressions as the ax language —
// an ISO date in since/until lands as the range boundary it names.
func TestSinceAndUntilBecomeTheRangeBoundaries(t *testing.T) {
	start, end, errMsg := parseTemporalParams("2025-01-01", "2025-02-01", "")

	require.Empty(t, errMsg)
	require.NotNil(t, start)
	require.NotNil(t, end)
	assert.Equal(t, time.Date(2025, 1, 1, 0, 0, 0, 0, start.Location()), *start)
	assert.Equal(t, time.Date(2025, 2, 1, 0, 0, 0, 0, end.Location()), *end)
}

// A boundary alone is a valid range: since with no until is open-ended.
func TestSinceAloneLeavesTheEndOpen(t *testing.T) {
	start, end, errMsg := parseTemporalParams("2025-01-01", "", "")

	require.Empty(t, errMsg)
	require.NotNil(t, start)
	assert.Nil(t, end)
}

// on names a day, so it spans that whole day — midnight to midnight — the
// same way the ax grammar's on clause resolves.
func TestOnSpansTheFullDay(t *testing.T) {
	start, end, errMsg := parseTemporalParams("", "", "2025-01-15")

	require.Empty(t, errMsg)
	require.NotNil(t, start)
	require.NotNil(t, end)
	assert.Equal(t, time.Date(2025, 1, 15, 0, 0, 0, 0, start.Location()), *start)
	assert.Equal(t, start.Add(24*time.Hour), *end)
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

// No temporal params means no temporal filter — absent, not zero.
func TestAbsentParamsLeaveTheFilterAbsent(t *testing.T) {
	start, end, errMsg := parseTemporalParams("", "", "")

	assert.Empty(t, errMsg)
	assert.Nil(t, start)
	assert.Nil(t, end)
}
