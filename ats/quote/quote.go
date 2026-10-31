// Package quote measures a quoted span against what the user said.
//
// A quoted span claims the user said it. ground checked the claim in a hook
// that blocked the write for up to twelve seconds; now the write passes, and
// the claim is checked after, against the prompts this node holds.
//
// "quote control needs to move to QNTX"
//
// "words like verdict and judge makes it feel like there is room for
// interpretation, there isnt in this case, the checks are mechanical"
//
// The measuring is Rust's (crates/ats/src/quote.rs). This build either carries
// it (qntxquote, quote_qntx.go) or does not (quote_default.go) and says which
// in Carried; there is no second implementation here to drift from the one the
// node runs. What a measurement is acceptable as is not decided here.
package quote

// Measurement is what was measured of one span against the prompts.
type Measurement struct {
	// Exact is a prompt containing the span, byte for byte.
	Exact bool
	// Corrections is the fewest between the span and the nearest stretch of any
	// prompt, in bytes added, dropped or changed. Zero when Exact. With no
	// prompt to compare against, every byte of the span is one.
	Corrections int
	// Length is the span's length in bytes.
	Length int
	// Prompt is the place in the list of the prompt the span is nearest to, the
	// one holding it when Exact; -1 when none was given or none is near.
	Prompt int
}
