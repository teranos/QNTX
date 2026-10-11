package types

import (
	"encoding/json"
	"time"

	"github.com/teranos/errors"
)

// "nil is nil"
//
// A query's time range names where it starts and where it ends: at a time, or
// unbounded on that side. Neither is held by a nil: a bound naming neither is
// refused where the query is read.

// boundKind is which side a TimeBound names.
type boundKind int

const (
	boundAt boundKind = iota + 1
	boundUnbounded
)

// unboundedJSON is how JSON says a side is unbounded.
const unboundedJSON = "unbounded"

// TimeBound is one end of a query's time range: the time it is bounded at,
// or unbounded on that side.
type TimeBound struct {
	kind boundKind
	at   time.Time
}

// BoundAt is a range bounded at t on this side.
func BoundAt(t time.Time) TimeBound {
	return TimeBound{kind: boundAt, at: t}
}

// Unbounded is a range unbounded on this side.
func Unbounded() TimeBound {
	return TimeBound{kind: boundUnbounded}
}

// At is the time this side is bounded at and true, or false where it is
// unbounded. A bound naming neither is refused, saying which end it is.
func (b TimeBound) At(end string) (time.Time, bool, error) {
	switch b.kind {
	case boundAt:
		return b.at, true, nil
	case boundUnbounded:
		return time.Time{}, false, nil
	}
	return time.Time{}, false, errors.Newf("the query names no %s of its time range: a time, or unbounded", end)
}

// MarshalJSON says the bound as its time, or as "unbounded". A bound naming
// neither is refused.
func (b TimeBound) MarshalJSON() ([]byte, error) {
	at, bounded, err := b.At("end")
	if err != nil {
		return nil, err
	}
	if bounded {
		return json.Marshal(at)
	}
	return json.Marshal(unboundedJSON)
}

// UnmarshalJSON reads a bound said as a time or as "unbounded". JSON null is
// left as it is, naming neither, so a filter kept before its bounds were
// named is refused where it is read.
func (b *TimeBound) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}
	var said string
	if err := json.Unmarshal(data, &said); err != nil {
		return errors.Wrapf(err, "a time bound is a time or %q, not %s", unboundedJSON, data)
	}
	if said == unboundedJSON {
		*b = Unbounded()
		return nil
	}
	at, err := time.Parse(time.RFC3339Nano, said)
	if err != nil {
		return errors.Wrapf(err, "a time bound is a time or %q, not %q", unboundedJSON, said)
	}
	*b = BoundAt(at)
	return nil
}
