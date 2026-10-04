package server

import "strings"

// A quoted span claims the user said it. ground checked the claim in a hook
// that blocked the write for up to twelve seconds; now the write passes, and
// the claim is checked here, against the prompts this node holds.

// quoteVerdict is what the prompts say about a quoted span.
type quoteVerdict int

const (
	quoteUnsourced quoteVerdict = iota
	quoteStretched
	quoteSourced
)

func (v quoteVerdict) String() string {
	switch v {
	case quoteSourced:
		return "sourced"
	case quoteStretched:
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

// quoteSpanMax is the longest span measured, as in ground.
const quoteSpanMax = 8192

// withinCorrections: whether some stretch of text sits within budget edits of
// span. Free to start and end anywhere, so the span is measured against the
// stretch it matches best rather than the whole prompt.
func withinCorrections(text, span string, budget int) bool {
	m := len(span)
	if m == 0 || m > quoteSpanMax {
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

// verdictOf is the nearest any prompt comes to the span: verbatim, inside the
// correction budget, inside the warn budget only, or not near at all.
func verdictOf(span string, said []string) quoteVerdict {
	if span == "" {
		return quoteUnsourced
	}
	for _, p := range said {
		if strings.Contains(p, span) {
			return quoteSourced
		}
	}
	clean, warn := correctionBudget(len(span)), warnBudget(len(span))
	if warn == 0 {
		return quoteUnsourced
	}
	nearest := quoteUnsourced
	for _, p := range said {
		if !withinCorrections(p, span, warn) {
			continue
		}
		if withinCorrections(p, span, clean) {
			return quoteSourced
		}
		nearest = quoteStretched
	}
	return nearest
}
