package quote

import (
	"testing"

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
		func(string) ([]Said, error) { return saidOf(prompts...), nil },
		substringMeasure, zap.NewNop().Sugar())
}

// A project no held namespace has a row of ground for is not known, and every
// span of it is unsourced: unprovenanced on arrival.
func TestArrivalOfAnUnknownProjectIsUnprovenanced(t *testing.T) {
	arrived, err := arrivalsWith(false, "the user typed this").Measure(Arrival{Project: "someone/zip", Ref: "abc", Texts: []string{`said "typed" here`}})
	if err != nil {
		t.Fatal(err)
	}
	if arrived.Known || len(arrived.Spans) != 1 || arrived.Spans[0].Kind != "unsourced" {
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
	if arrived.Spans[0].Kind != "sourced" || arrived.Spans[1].Kind != "unsourced" {
		t.Errorf("spans = %+v", arrived.Spans)
	}
	// The exact span says whose words it is, in which row, when, in which session.
	if s := arrived.Spans[0]; s.Who != "did:key:person" || s.Row != "row-1" || s.Session != "sess-0" || s.At == "" {
		t.Errorf("the matched span does not name its row: %+v", s)
	}
	if s := arrived.Spans[1]; s.Who != "" || s.Row != "" {
		t.Errorf("an unsourced span named a row: %+v", s)
	}
}

func TestArrivalWithoutQuotesAsksNothing(t *testing.T) {
	arrived, err := arrivalsWith(true).Measure(Arrival{Project: "teranos/QNTX", Texts: []string{"plain words"}})
	if err != nil || len(arrived.Spans) != 0 || arrived.Namespace != "" {
		t.Errorf("arrived = %+v, %v", arrived, err)
	}
}
