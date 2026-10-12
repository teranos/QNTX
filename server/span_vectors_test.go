package server

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// The status line shows spans as Rust writes them: every length in
// crates/ats/testdata/spans.json, which crates/ats checks it writes too.
func TestShortAgoWritesTheSpanVectors(t *testing.T) {
	path := "../crates/ats/testdata/spans.json"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var vectors struct {
		OfMs []struct {
			Ms    int64  `json:"ms"`
			Short string `json:"short"`
		} `json:"of_ms"`
		Readout []struct {
			Ms     int64  `json:"ms"`
			Finest string `json:"finest"`
			Short  string `json:"short"`
		} `json:"readout"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	for _, v := range vectors.OfMs {
		if got := shortAgo(time.Duration(v.Ms) * time.Millisecond); got != v.Short {
			t.Errorf("shortAgo(%dms) = %q, want %q", v.Ms, got, v.Short)
		}
	}
	finest := map[string]time.Duration{"ms": time.Millisecond, "s": time.Second, "m": time.Minute}
	for _, v := range vectors.Readout {
		per, named := finest[v.Finest]
		if !named {
			t.Fatalf("%s names finest %q, which this test does not read", path, v.Finest)
		}
		if got := readout(time.Duration(v.Ms)*time.Millisecond, per); got != v.Short {
			t.Errorf("readout(%dms, %s) = %q, want %q", v.Ms, v.Finest, got, v.Short)
		}
	}
}
