package quote

import (
	"strconv"
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
		exact, at := false, -1
		for i, prompt := range said {
			if strings.Contains(prompt, span) && !exact {
				exact, at = true, i
			}
		}
		corrections := len(span)
		if exact {
			corrections = 0
		}
		measured = append(measured, quoted.Measurement{Exact: exact, Corrections: corrections, Length: len(span), Prompt: at})
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

// saidOf is prompts as a namespace would hold them, each in a row of its own.
func saidOf(texts ...string) []Said {
	said := make([]Said, 0, len(texts))
	for i, text := range texts {
		said = append(said, Said{Text: text, Row: "row-" + strconv.Itoa(i+1), Who: "did:key:person", At: "2026-10-31T10:00:00Z", Session: "sess-0"})
	}
	return said
}
