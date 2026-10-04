package server

import "testing"

// "quote control needs to move to QNTX"
// The rules are ground's, unchanged: verbatim first, then four corrections per
// forty characters, five or six warned, past six not at all.

func TestQuoteBudgets(t *testing.T) {
	cases := []struct{ length, clean, warn int }{
		{40, 4, 6}, {39, 3, 5}, {41, 4, 6}, {80, 8, 12}, {10, 1, 1}, {9, 0, 1}, {1, 0, 0}, {0, 0, 0},
	}
	for _, c := range cases {
		if got := correctionBudget(c.length); got != c.clean {
			t.Errorf("correctionBudget(%d) = %d, want %d", c.length, got, c.clean)
		}
		if got := warnBudget(c.length); got != c.warn {
			t.Errorf("warnBudget(%d) = %d, want %d", c.length, got, c.warn)
		}
	}
}

func TestWithinCorrections(t *testing.T) {
	said := "the user typed this"
	cases := []struct {
		span   string
		budget int
		want   bool
	}{
		{"typed", 0, true},
		{"tyPed", 0, false},
		{"tyPed", 1, true},
		{"tYPed", 1, false},
		{"tYPed", 2, true},
		{"wrote that", 2, false},
		{"", 4, false},
	}
	for _, c := range cases {
		if got := withinCorrections(said, c.span, c.budget); got != c.want {
			t.Errorf("withinCorrections(%q, %q, %d) = %v, want %v", said, c.span, c.budget, got, c.want)
		}
	}
	if !withinCorrections("the user typd this", "typed", 1) {
		t.Error("a dropped character is one correction")
	}
	if !withinCorrections("the user typeed this", "typed", 1) {
		t.Error("an added character is one correction")
	}
}

func TestQuoteVerdict(t *testing.T) {
	said := []string{"nothing near", "this is what the user actually typed ok!"}
	cases := []struct {
		span string
		want quoteVerdict
	}{
		{"what the user actually typed", quoteSourced},
		{"This is what the user actuaIIy typed ok?", quoteSourced},
		{"This is what the user actuaIIy tiped ok?", quoteStretched},
		{"This is What the user actuaIIy tiped ok?", quoteStretched},
		{"This is What the User actuaIIy tiped ok?", quoteUnsourced},
		{"no prompt says anything like this at all", quoteUnsourced},
	}
	for _, c := range cases {
		if got := verdictOf(c.span, said); got != c.want {
			t.Errorf("verdictOf(%q) = %v, want %v", c.span, got, c.want)
		}
	}
	// The nearest prompt decides, wherever it falls among them.
	near := []string{"This is What the user actuaIIy tiped ok?", "this is what the user actually typed ok!"}
	if got := verdictOf("This is what the user actuaIIy typed ok?", near); got != quoteSourced {
		t.Errorf("a later, nearer prompt was not the one that decided: %v", got)
	}
}
