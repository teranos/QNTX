//go:build qntxquote

package quote

import (
	"encoding/json"
	"os"
	"testing"
)

// testdata/quote.json is what Rust writes and Go checks it reads the same way:
// whether each span is exact, the corrections to the nearest prompt, and its
// length.
func TestMeasureReadsTheVectors(t *testing.T) {
	path := "../../crates/ats/testdata/quote.json"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var vectors struct {
		Measurements []struct {
			Said        []string `json:"said"`
			Span        string   `json:"span"`
			Exact       bool     `json:"exact"`
			Corrections int      `json:"corrections"`
			Length      int      `json:"length"`
			Prompt      *int     `json:"prompt"`
		} `json:"measurements"`
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	for _, v := range vectors.Measurements {
		got, err := Measure([]string{v.Span}, v.Said)
		if err != nil {
			t.Errorf("Measure(%q): %v", v.Span, err)
			continue
		}
		want := Measurement{Exact: v.Exact, Corrections: v.Corrections, Length: v.Length, Prompt: promptAt(v.Prompt)}
		if len(got) != 1 || got[0] != want {
			t.Errorf("Measure(%q) = %v, want %v", v.Span, got, want)
		}
	}
}

// Spans cross together: each one's measurement is its own, in order.
func TestMeasureAnswersEachSpanInOrder(t *testing.T) {
	got, err := Measure([]string{"typed", "never said at all", "typed"}, []string{"the user typed this"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("measured %d of 3", len(got))
	}
	if !got[0].Exact || got[1].Exact || !got[2].Exact {
		t.Errorf("exactness out of order: %v", got)
	}
	if got[1].Corrections == 0 || got[1].Length != len("never said at all") {
		t.Errorf("the span that matched nothing was measured %v", got[1])
	}
}
