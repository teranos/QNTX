package quote

import (
	"testing"

	quoted "github.com/teranos/QNTX/ats/quote"
)

// The numbers are ground's: exact first, then four corrections per forty bytes
// are taken as the user's words, up to six are reported as stretched, past six
// the span has no source.
func TestPolicyNamesAMeasurement(t *testing.T) {
	cases := []struct {
		name string
		m    quoted.Measurement
		want string
	}{
		{"exact", quoted.Measurement{Exact: true, Length: 28}, "sourced"},
		{"four per forty", quoted.Measurement{Corrections: 4, Length: 40}, "sourced"},
		{"five per forty", quoted.Measurement{Corrections: 5, Length: 40}, "stretched"},
		{"six per forty", quoted.Measurement{Corrections: 6, Length: 40}, "stretched"},
		{"seven per forty", quoted.Measurement{Corrections: 7, Length: 40}, "unsourced"},
		{"every byte", quoted.Measurement{Corrections: 40, Length: 40}, "unsourced"},
		{"under ten bytes buys no correction", quoted.Measurement{Corrections: 1, Length: 9}, "stretched"},
		{"one byte buys none at all", quoted.Measurement{Corrections: 1, Length: 1}, "unsourced"},
		{"nothing measured", quoted.Measurement{}, "unsourced"},
		{"too long for corrections", quoted.Measurement{Corrections: 1, Length: spanMax + 1}, "unsourced"},
		{"too long but exact", quoted.Measurement{Exact: true, Length: spanMax + 1}, "sourced"},
	}
	for _, c := range cases {
		if got := kind(c.m); got != c.want {
			t.Errorf("%s: %+v is %s, want %s", c.name, c.m, got, c.want)
		}
	}
}

// What is reported as the user's words is what is accepted or stretched.
func TestPolicyNearIsAcceptedOrStretched(t *testing.T) {
	for _, m := range []quoted.Measurement{
		{Exact: true, Length: 5}, {Corrections: 4, Length: 40}, {Corrections: 6, Length: 40}, {Corrections: 7, Length: 40}, {},
	} {
		want := kind(m) != "unsourced"
		if near(m) != want {
			t.Errorf("%+v: near is %v, but it is %s", m, near(m), kind(m))
		}
	}
}
