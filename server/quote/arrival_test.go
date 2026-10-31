package quote

import (
	"testing"

	quoted "github.com/teranos/QNTX/ats/quote"
	"go.uber.org/zap"
)

func TestSpansAreTheDoubleQuotedOnes(t *testing.T) {
	got := Spans(`he said "one thing" and then "another", 「[Fable] not "this one"」 but “this too”`)
	want := []string{"one thing", "another", "this too"}
	if len(got) != len(want) {
		t.Fatalf("Spans = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("span %d = %q, want %q", i, got[i], want[i])
		}
	}
	if n := len(Spans("no quotes here")); n != 0 {
		t.Errorf("found %d spans in a text without quotes", n)
	}
}

func arrivalsWith(known bool, prompts ...string) *Arrivals {
	return NewArrivals(
		func(project string) (string, bool) { return "ground", known },
		func(string) ([]string, error) { return prompts, nil },
		substringJudge, zap.NewNop().Sugar())
}

// A project no held namespace has a row of ground for is not known, and every
// span of it is unsourced: unprovenanced on arrival.
func TestArrivalOfAnUnknownProjectIsUnprovenanced(t *testing.T) {
	arrived, err := arrivalsWith(false, "the user typed this").Measure(Arrival{Project: "someone/zip", Ref: "abc", Texts: []string{`said "typed" here`}})
	if err != nil {
		t.Fatal(err)
	}
	if arrived.Known || len(arrived.Spans) != 1 || arrived.Spans[0].Verdict != quoted.Unsourced.String() {
		t.Errorf("arrived = %+v", arrived)
	}
}

// A project ground has rows for is measured against that namespace's prompts.
func TestArrivalOfAKnownProjectIsMeasured(t *testing.T) {
	arrived, err := arrivalsWith(true, "the user typed this").Measure(Arrival{Project: "teranos/QNTX", Ref: "abc", Texts: []string{`commit says "typed" and "never said"`}})
	if err != nil {
		t.Fatal(err)
	}
	if !arrived.Known || arrived.Namespace != "ground" || len(arrived.Spans) != 2 {
		t.Fatalf("arrived = %+v", arrived)
	}
	if arrived.Spans[0].Verdict != quoted.Sourced.String() || arrived.Spans[1].Verdict != quoted.Unsourced.String() {
		t.Errorf("spans = %+v", arrived.Spans)
	}
}

func TestArrivalWithoutQuotesAsksNothing(t *testing.T) {
	arrived, err := arrivalsWith(true).Measure(Arrival{Project: "teranos/QNTX", Texts: []string{"plain words"}})
	if err != nil || len(arrived.Spans) != 0 || arrived.Namespace != "" {
		t.Errorf("arrived = %+v, %v", arrived, err)
	}
}
