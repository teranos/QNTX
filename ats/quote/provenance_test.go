package quote

import "testing"

func TestVerdictsAreNamedAsRustNamesThem(t *testing.T) {
	for _, v := range []Verdict{Unsourced, Stretched, Sourced} {
		named, known := verdictNamed(v.String())
		if !known || named != v {
			t.Errorf("%v is written %q and read back %v (%v)", v, v.String(), named, known)
		}
	}
	if _, known := verdictNamed("invented"); known {
		t.Error("a name Rust does not write was read as a verdict")
	}
}

func TestOnlyAMeasuredJudgementHasCorrections(t *testing.T) {
	if _, measured := Judged(Sourced, 0, false).Corrections(); measured {
		t.Error("a sourced span reported being measured")
	}
	if n, measured := Judged(Stretched, 5, true).Corrections(); !measured || n != 5 {
		t.Errorf("a measured span read back %d (%v)", n, measured)
	}
}
