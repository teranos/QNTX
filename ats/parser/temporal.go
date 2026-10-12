package parser

import (
	"time"
)

// timeNow is what relative expressions resolve against; tests set it.
var timeNow = time.Now

// ParseTemporalExpression resolves "yesterday", "last month", "3 days ago" or
// "2025-01-15" to the moment it names. There is one resolver, in
// crates/ats/src/temporal.rs: the ax language and every since/until here read
// time the same way, so `last month` is one month whichever door it came in by.
func ParseTemporalExpression(expr string) (*time.Time, error) {
	ms, err := resolveTemporalDispatch(expr, timeNow())
	if err != nil {
		return nil, err
	}
	t := timeAtMs(ms)
	return &t, nil
}

// ParseTemporalDay resolves an expression to the UTC calendar day it falls on,
// start inclusive and end exclusive: `on yesterday` is yesterday midnight to
// midnight, as the ax grammar's on clause reads it.
func ParseTemporalDay(expr string) (start, end time.Time, err error) {
	startMs, endMs, err := resolveTemporalDayDispatch(expr, timeNow())
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return timeAtMs(startMs), timeAtMs(endMs), nil
}

// timeAtMs is the UTC moment epoch milliseconds name; the resolver reads time
// in UTC.
func timeAtMs(ms int64) time.Time {
	return time.UnixMilli(ms).UTC()
}
