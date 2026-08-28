package storage

import (
	"time"

	"github.com/teranos/errors"
)

// A timestamp column that parses itself.

// Written as `time.Parse(...)` at a call site, a timestamp that will not parse
// yields the zero time and the row reads as dated year 1 — sorting first,
// looking like the oldest thing in the store. There were seven of those.

// Scanning through this makes the parse part of reading the column, so the
// error arrives in the rows.Scan the caller already checks and there is no
// second thing to remember.
type RFC3339 struct{ into *time.Time }

// At names the field the timestamp lands in: rows.Scan(..., At(&x.CreatedAt)).
func At(into *time.Time) RFC3339 { return RFC3339{into: into} }

func (r RFC3339) Scan(src any) error {
	if r.into == nil {
		return errors.New("a timestamp was scanned into nowhere")
	}
	switch v := src.(type) {
	case nil:
		*r.into = time.Time{}
		return nil
	case time.Time:
		*r.into = v
		return nil
	case string:
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return errors.Wrapf(err, "the timestamp %q is not RFC3339", v)
		}
		*r.into = parsed
		return nil
	case []byte:
		parsed, err := time.Parse(time.RFC3339, string(v))
		if err != nil {
			return errors.Wrapf(err, "the timestamp %q is not RFC3339", v)
		}
		*r.into = parsed
		return nil
	default:
		return errors.Newf("a timestamp column arrived as %T, which cannot be read as one", src)
	}
}
