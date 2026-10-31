package quote

import (
	"strings"

	quoted "github.com/teranos/QNTX/ats/quote"
)

// The verdict is Rust's (ats/quote), and these handlers are tested without it:
// a judge that says what the test needs it to, so they run in every build.

// substringJudge sources a span the prompts hold verbatim and calls any other
// unsourced, as far from the prompts as it is long.
func substringJudge(spans, said []string) ([]quoted.Judgement, error) {
	judged := make([]quoted.Judgement, 0, len(spans))
	for _, span := range spans {
		sourced := false
		for _, prompt := range said {
			if strings.Contains(prompt, span) {
				sourced = true
			}
		}
		if sourced {
			judged = append(judged, quoted.Judged(quoted.Sourced, 0, false))
			continue
		}
		judged = append(judged, quoted.Judged(quoted.Unsourced, len(span), true))
	}
	return judged, nil
}

// judgeAs calls every span the verdict, measured one correction away unless it
// is sourced.
func judgeAs(v quoted.Verdict) func(spans, said []string) ([]quoted.Judgement, error) {
	return func(spans, _ []string) ([]quoted.Judgement, error) {
		judged := make([]quoted.Judgement, 0, len(spans))
		for range spans {
			judged = append(judged, quoted.Judged(v, 1, v != quoted.Sourced))
		}
		return judged, nil
	}
}
