//go:build qntxwasm

package quote

import (
	"encoding/json"
	"os"
	"testing"
)

// testdata/quote.json is what Rust writes and Go checks it reads the same way:
// every verdict, and how far each unsourced or stretched span was measured.
func TestJudgeReadsTheVectors(t *testing.T) {
	path := "../../crates/ats/testdata/quote.json"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var vectors struct {
		Verdicts []struct {
			Said    []string `json:"said"`
			Span    string   `json:"span"`
			Verdict string   `json:"verdict"`
			Degree  *int     `json:"degree"`
		} `json:"verdicts"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	for _, v := range vectors.Verdicts {
		got, err := Judge([]string{v.Span}, v.Said)
		if err != nil {
			t.Errorf("Judge(%q): %v", v.Span, err)
			continue
		}
		if len(got) != 1 || got[0].Verdict.String() != v.Verdict {
			t.Errorf("Judge(%q) = %v, want %s", v.Span, got, v.Verdict)
			continue
		}
		degree, measured := got[0].Corrections()
		if v.Verdict == "sourced" {
			// Rust measures a sourced span in the vectors (degree_of); Judge does not.
			if measured {
				t.Errorf("Judge(%q) measured a sourced span at %d", v.Span, degree)
			}
			continue
		}
		if !measured || (v.Degree != nil && degree != *v.Degree) {
			t.Errorf("Judge(%q) measured %d (%v), want %v", v.Span, degree, measured, v.Degree)
		}
	}
}

// Spans cross together: each one's verdict is its own, in order.
func TestJudgeAnswersEachSpanInOrder(t *testing.T) {
	got, err := Judge([]string{"typed", "never said at all", "typed"}, []string{"the user typed this"})
	if err != nil {
		t.Fatal(err)
	}
	want := []Verdict{Sourced, Unsourced, Sourced}
	if len(got) != len(want) {
		t.Fatalf("judged %d of %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Verdict != want[i] {
			t.Errorf("span %d = %v, want %v", i, got[i].Verdict, want[i])
		}
	}
}
