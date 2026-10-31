// Package quote is what the prompts say about a quoted span.
//
// A quoted span claims the user said it. ground checked the claim in a hook
// that blocked the write for up to twelve seconds; now the write passes, and
// the claim is checked after, against the prompts this node holds.
//
// "quote control needs to move to QNTX"
//
// The check is Rust's (crates/ats/src/quote.rs). This build either carries it
// (qntxquote, quote_qntx.go) or does not (quote_default.go) and says which in
// Carried; there is no second implementation here to drift from the one the
// node runs.
package quote

import "strings"

// Verdict is what the prompts say about a quoted span.
type Verdict int

const (
	Unsourced Verdict = iota
	Stretched
	Sourced
)

func (v Verdict) String() string {
	switch v {
	case Sourced:
		return "sourced"
	case Stretched:
		return "stretched"
	default:
		return "unsourced"
	}
}

// verdictNamed is the verdict Rust wrote the name of. A name Rust does not
// write is not a verdict.
func verdictNamed(name string) (Verdict, bool) {
	switch strings.TrimSpace(name) {
	case "sourced":
		return Sourced, true
	case "stretched":
		return Stretched, true
	case "unsourced":
		return Unsourced, true
	}
	return Unsourced, false
}

// Judgement is one span's verdict. A stretched or unsourced span is measured:
// how many corrections lie between it and the nearest stretch of any prompt.
// A sourced one is not.
type Judgement struct {
	Verdict     Verdict
	corrections int
	measured    bool
}

// Corrections is how far a stretched or unsourced span is from the nearest
// stretch of any prompt, and false for a sourced span, which is not measured.
func (j Judgement) Corrections() (int, bool) {
	return j.corrections, j.measured
}

// Judged is a judgement a test can write down without Rust: the verdict, and
// how far it was measured to be when it was measured.
func Judged(v Verdict, corrections int, measured bool) Judgement {
	return Judgement{Verdict: v, corrections: corrections, measured: measured}
}
