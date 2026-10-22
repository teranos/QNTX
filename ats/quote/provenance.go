package quote

import "strings"

// A quoted span claims the user said it. ground checked the claim in a hook
// that blocked the write for up to twelve seconds; now the write passes, and
// the claim is checked here, against the prompts this node holds.

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

// correctionBudget is four corrections per forty characters of the span,
// floored. A span under ten characters buys none and must be verbatim.
func correctionBudget(length int) int { return length * 4 / 40 }

// warnBudget is the band above it that still passes, said rather than silent:
// five and six per forty. Past it the span has no source.
func warnBudget(length int) int { return length * 6 / 40 }

// spanMax is the longest span measured, as in ground.
const spanMax = 8192

// withinCorrections: whether some stretch of text sits within budget edits of
// span. Free to start and end anywhere, so the span is measured against the
// stretch it matches best rather than the whole prompt.
func withinCorrections(text, span string, budget int) bool {
	m := len(span)
	if m == 0 || m > spanMax {
		return false
	}
	prev := make([]int, m+1)
	cur := make([]int, m+1)
	for i := range prev {
		prev[i] = i
	}
	for t := 0; t < len(text); t++ {
		c := text[t]
		cur[0] = 0
		for i := 1; i <= m; i++ {
			best := prev[i-1]
			if c != span[i-1] {
				best++
			}
			if prev[i]+1 < best {
				best = prev[i] + 1
			}
			if cur[i-1]+1 < best {
				best = cur[i-1] + 1
			}
			cur[i] = best
		}
		if cur[m] <= budget {
			return true
		}
		prev, cur = cur, prev
	}
	return false
}

// DegreeOf is how many corrections separate the span from the nearest stretch
// of any prompt. With nothing to compare against, every character is one.
func DegreeOf(span string, said []string) int {
	best := len(span)
	for _, p := range said {
		if d := nearestCorrections(p, span); d < best {
			best = d
		}
	}
	return best
}

// nearestCorrections is the fewest corrections that make span any stretch of
// text: the table withinCorrections walks, kept to its smallest end.
func nearestCorrections(text, span string) int {
	m := len(span)
	if m == 0 {
		return 0
	}
	prev := make([]int, m+1)
	cur := make([]int, m+1)
	for i := range prev {
		prev[i] = i
	}
	best := m
	for t := 0; t < len(text); t++ {
		c := text[t]
		cur[0] = 0
		for i := 1; i <= m; i++ {
			d := prev[i-1]
			if c != span[i-1] {
				d++
			}
			if prev[i]+1 < d {
				d = prev[i] + 1
			}
			if cur[i-1]+1 < d {
				d = cur[i-1] + 1
			}
			cur[i] = d
		}
		if cur[m] < best {
			best = cur[m]
		}
		prev, cur = cur, prev
	}
	return best
}

// VerdictOf is the nearest any prompt comes to the span: verbatim, inside the
// correction budget, inside the warn budget only, or not near at all.
func VerdictOf(span string, said []string) Verdict {
	if span == "" {
		return Unsourced
	}
	for _, p := range said {
		if strings.Contains(p, span) {
			return Sourced
		}
	}
	clean, warn := correctionBudget(len(span)), warnBudget(len(span))
	if warn == 0 {
		return Unsourced
	}
	nearest := Unsourced
	for _, p := range said {
		if !withinCorrections(p, span, warn) {
			continue
		}
		if withinCorrections(p, span, clean) {
			return Sourced
		}
		nearest = Stretched
	}
	return nearest
}
