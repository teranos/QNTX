//go:build qntxquote

package quote

import (
	"encoding/json"

	"github.com/teranos/QNTX/ats/wasm"
	errors "github.com/teranos/sacred-error"
)

// Carried is whether this build holds the Rust that judges quotes.
const Carried = true

type judgeInput struct {
	Spans []string `json:"spans"`
	Said  []string `json:"said"`
}

type judgeOutput struct {
	Judgements []struct {
		Verdict     string `json:"verdict"`
		Corrections *int   `json:"corrections"`
	} `json:"judgements"`
	Error string `json:"error"`
}

// Judge is what the prompts say of each span, in order. The prompts cross to
// Rust once however many spans there are.
func Judge(spans, said []string) ([]Judgement, error) {
	engine, err := wasm.GetEngine()
	if err != nil {
		return nil, errors.Wrapf(err, "judging %d quoted spans: the wasm engine is not there", len(spans))
	}
	// A Go slice holding nothing is written null, and Rust reads a list.
	in, err := json.Marshal(judgeInput{
		Spans: append(make([]string, 0, len(spans)), spans...),
		Said:  append(make([]string, 0, len(said)), said...),
	})
	if err != nil {
		return nil, errors.Wrapf(err, "judging %d quoted spans against %d prompts: input did not encode", len(spans), len(said))
	}
	raw, err := engine.Call("quote_judge", string(in))
	if err != nil {
		return nil, errors.Wrapf(err, "judging %d quoted spans against %d prompts", len(spans), len(said))
	}
	var out judgeOutput
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, errors.Wrapf(err, "quote_judge answered something that is not judgements: %.200s", raw)
	}
	if out.Error != "" {
		return nil, errors.Newf("quote_judge refused %d spans against %d prompts: %s", len(spans), len(said), out.Error)
	}
	if len(out.Judgements) != len(spans) {
		return nil, errors.Newf("quote_judge judged %d of %d spans", len(out.Judgements), len(spans))
	}
	judged := make([]Judgement, 0, len(spans))
	for i, j := range out.Judgements {
		v, known := verdictNamed(j.Verdict)
		if !known {
			return nil, errors.Newf("quote_judge named verdict %q for span %d of %d, which is none of the three", j.Verdict, i+1, len(spans))
		}
		judgement := Judgement{Verdict: v}
		if j.Corrections != nil {
			judgement.corrections, judgement.measured = *j.Corrections, true
		}
		judged = append(judged, judgement)
	}
	return judged, nil
}
