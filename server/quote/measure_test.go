package quote

import (
	"strings"

	quoted "github.com/teranos/QNTX/ats/quote"
)

// The measuring is Rust's (ats/quote), and these handlers are tested without
// it: a measure that says what the test needs it to, so they run in every build.

// substringMeasure finds a span exact where a prompt holds it byte for byte,
// and any other as far from the prompts as it is long.
func substringMeasure(spans, said []string) ([]quoted.Measurement, error) {
	measured := make([]quoted.Measurement, 0, len(spans))
	for _, span := range spans {
		exact := false
		for _, prompt := range said {
			if strings.Contains(prompt, span) {
				exact = true
			}
		}
		corrections := len(span)
		if exact {
			corrections = 0
		}
		measured = append(measured, quoted.Measurement{Exact: exact, Corrections: corrections, Length: len(span)})
	}
	return measured, nil
}

// measuredAs measures every span as m, whatever the span.
func measuredAs(m quoted.Measurement) func(spans, said []string) ([]quoted.Measurement, error) {
	return func(spans, _ []string) ([]quoted.Measurement, error) {
		measured := make([]quoted.Measurement, 0, len(spans))
		for range spans {
			measured = append(measured, m)
		}
		return measured, nil
	}
}
