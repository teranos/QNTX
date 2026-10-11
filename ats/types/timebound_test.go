package types

import (
	"encoding/json"
	"testing"
	"time"
)

// "nil is nil": a query's time range names each side, a time or unbounded,
// and a side naming neither is refused, not read as unbounded.
func TestATimeRangeNamesEachSide(t *testing.T) {
	at := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)

	if got, bounded, err := BoundAt(at).At("start"); err != nil || !bounded || !got.Equal(at) {
		t.Errorf("a bound at %v read as %v, %v, %v", at, got, bounded, err)
	}
	if _, bounded, err := Unbounded().At("start"); err != nil || bounded {
		t.Errorf("an unbounded side read as bounded %v, %v", bounded, err)
	}
	if _, _, err := (TimeBound{}).At("start"); err == nil {
		t.Error("a side naming neither was read")
	}

	for _, b := range []TimeBound{BoundAt(at), Unbounded()} {
		said, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("%v did not marshal: %v", b, err)
		}
		var read TimeBound
		if err := json.Unmarshal(said, &read); err != nil {
			t.Fatalf("%s did not unmarshal: %v", said, err)
		}
		if read != b {
			t.Errorf("%s read back as %v, want %v", said, read, b)
		}
	}
	if _, err := json.Marshal(TimeBound{}); err == nil {
		t.Error("a side naming neither was marshalled")
	}

	// A filter kept before its bounds were named says null, which names
	// neither.
	var kept AxFilter
	if err := json.Unmarshal([]byte(`{"time_start":null}`), &kept); err != nil {
		t.Fatalf("a kept filter did not unmarshal: %v", err)
	}
	if _, _, err := kept.TimeStart.At("start"); err == nil {
		t.Error("a null start was read as a side")
	}
}
