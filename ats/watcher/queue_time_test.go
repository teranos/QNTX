package watcher_test

import (
	"testing"
	"time"
)

// The queue compares timestamps as strings in SQL. time.RFC3339Nano strips
// trailing zeros, so a later time can compare as smaller and the row is never
// selected — completed entries never purge, queued entries never become due.
const sqlTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

func TestTimestampsSortInTimeOrder(t *testing.T) {
	base := time.Date(2026, 8, 6, 21, 0, 0, 0, time.UTC)

	// 500ms then 530ms: RFC3339Nano writes ".5Z" and ".53Z", and 'Z' sorts
	// after '3', so the earlier time compares as the larger string.
	earlier := base.Add(500 * time.Millisecond)
	later := base.Add(530 * time.Millisecond)

	if !earlier.Before(later) {
		t.Fatal("the fixture is wrong: earlier is not before later")
	}

	if e, l := earlier.Format(sqlTimeFormat), later.Format(sqlTimeFormat); e >= l {
		t.Errorf("a row written at %s would not be selected by a cutoff of %s", e, l)
	}

	// The format this replaced, kept as the reason it was replaced.
	if e, l := earlier.Format(time.RFC3339Nano), later.Format(time.RFC3339Nano); e < l {
		t.Errorf("RFC3339Nano sorted %s before %s, so this test no longer guards anything", e, l)
	}
}

func TestTimestampWidthIsConstant(t *testing.T) {
	base := time.Date(2026, 8, 6, 21, 0, 0, 0, time.UTC)
	width := len(base.Format(sqlTimeFormat))

	// Every fraction a clock can produce has to occupy the same width, or the
	// comparison stops being lexicographic in the same way.
	for _, ns := range []int{0, 1, 500, 1_000_000, 500_000_000, 999_999_999} {
		got := base.Add(time.Duration(ns)).Format(sqlTimeFormat)
		if len(got) != width {
			t.Errorf("%d ns formatted to %s, width %d, want %d", ns, got, len(got), width)
		}
	}
}
