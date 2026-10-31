//go:build qntxquote

package quote

import (
	"encoding/json"

	"github.com/teranos/QNTX/ats/wasm"
	errors "github.com/teranos/sacred-error"
)

// Carried is whether this build holds the Rust that measures quotes.
const Carried = true

type measureInput struct {
	Spans []string `json:"spans"`
	Said  []string `json:"said"`
}

type measureOutput struct {
	Measurements []struct {
		Exact       bool `json:"exact"`
		Corrections int  `json:"corrections"`
		Length      int  `json:"length"`
	} `json:"measurements"`
	Error string `json:"error"`
}

// Measure is what was measured of each span against the prompts, in order. The
// prompts cross to Rust once however many spans there are.
// IMPLEMENTME: fuzzy and semantic matching beside edit distance, inside qntxquote
// with the types that suit them; crates/ats/src/similarity.rs already holds cosine
// similarity. Whatever matches inside, the boundary speaks protocol.Quote.
func Measure(spans, said []string) ([]Measurement, error) {
	engine, err := wasm.GetEngine()
	if err != nil {
		return nil, errors.Wrapf(err, "measuring %d quoted spans: the wasm engine is not there", len(spans))
	}
	// A Go slice holding nothing is written null, and Rust reads a list.
	in, err := json.Marshal(measureInput{
		Spans: append(make([]string, 0, len(spans)), spans...),
		Said:  append(make([]string, 0, len(said)), said...),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "measuring %d quoted spans against %d prompts: input did not encode", len(spans), len(said))
	}
	raw, err := engine.Call("quote_measure", string(in))
	if err != nil {
		return nil, errors.Wrapf(err, "measuring %d quoted spans against %d prompts", len(spans), len(said))
	}
	var out measureOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, errors.Wrapf(err, "quote_measure answered something that is not measurements: %.200s", raw)
	}
	if out.Error != "" {
		return nil, errors.Newf("quote_measure refused %d spans against %d prompts: %s", len(spans), len(said), out.Error)
	}
	if len(out.Measurements) != len(spans) {
		return nil, errors.Newf("quote_measure measured %d of %d spans", len(out.Measurements), len(spans))
	}
	measured := make([]Measurement, 0, len(spans))
	for _, m := range out.Measurements {
		measured = append(measured, Measurement{Exact: m.Exact, Corrections: m.Corrections, Length: m.Length})
	}
	return measured, nil
}
