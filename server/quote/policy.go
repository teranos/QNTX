package quote

import quoted "github.com/teranos/QNTX/ats/quote"

// The numbers the policy is made of, all of them, so none is read out of a
// formula below.
const (
	// window: the rates below are per this many bytes of a span.
	window = 40

	// acceptedPerWindow: corrections per window a span may be from the prompts
	// and still be taken as the user's words.
	acceptedPerWindow = 4

	// nearPerWindow: corrections per window a span may be from the prompts and
	// still be reported as the user's words, stretched. Past it the span has no
	// source.
	nearPerWindow = 6

	// spanMax: the longest span taken by corrections. A longer one is the
	// user's words only when a prompt holds it byte for byte.
	spanMax = 8192
)

// Policy is what is acceptable of a measurement. The numbers are ground's,
// ported with no reason recorded anywhere in this repository.
// "4 ? 10 ?" "40 ?"
//
// IMPLEMENTME: policy does not belong with the measuring. It moves to where
// ADR-050 puts policy, a control a person configures, with the model on the
// enforced side of it; until then it is these numbers, in this one place.

// accepts is whether the measurement is taken as the user's words.
func accepts(m quoted.Measurement) bool {
	if m.Exact {
		return true
	}
	return m.Length > 0 && m.Length <= spanMax && m.Corrections <= m.Length*acceptedPerWindow/window
}

// near is whether the measurement is reported as the user's words, accepted or
// stretched.
func near(m quoted.Measurement) bool {
	if accepts(m) {
		return true
	}
	return m.Length > 0 && m.Length <= spanMax && m.Corrections <= m.Length*nearPerWindow/window
}

// kind is how the policy names a measurement: sourced, stretched or unsourced.
func kind(m quoted.Measurement) string {
	switch {
	case accepts(m):
		return "sourced"
	case near(m):
		return "stretched"
	default:
		return "unsourced"
	}
}
