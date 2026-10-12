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
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	for _, v := range vectors.OfMs {
		if got := shortAgo(time.Duration(v.Ms) * time.Millisecond); got != v.Short {
			t.Errorf("shortAgo(%dms) = %q, want %q", v.Ms, got, v.Short)
		}
	}
}
