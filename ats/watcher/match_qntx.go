//go:build qntxwasm

package watcher

import (
	"encoding/json"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/wasm"
	errors "github.com/teranos/sacred-error"
)

// wasmMatchAttestation is the JSON shape Rust expects.
type wasmMatchAttestation struct {
	Subjects    []string       `json:"subjects"`
	Predicates  []string       `json:"predicates"`
	Contexts    []string       `json:"contexts"`
	Actors      []string       `json:"actors"`
	TimestampMs int64          `json:"timestamp_ms"`
	Attributes  map[string]any `json:"attributes,omitempty"`
}

// wasmWatcherFilter is the JSON shape for a single watcher filter.
type wasmWatcherFilter struct {
	ID               string                    `json:"id"`
	Subjects         []string                  `json:"subjects"`
	Predicates       []string                  `json:"predicates"`
	Contexts         []string                  `json:"contexts"`
	Actors           []string                  `json:"actors"`
	TimeStartMs      *int64                    `json:"time_start_ms,omitempty"`
	TimeEndMs        *int64                    `json:"time_end_ms,omitempty"`
	AttributeFilters []storage.AttributeFilter `json:"attribute_filters"`
	Over             *string                   `json:"over,omitempty"`
}

// wasmMatchInput is the full input for match_watchers. Types are the
// definitions of the types the attestation is of, once read.
type wasmMatchInput struct {
	Attestation wasmMatchAttestation    `json:"attestation"`
	Watchers    []wasmWatcherFilter     `json:"watchers"`
	Types       *[]wasmMatchAttestation `json:"types,omitempty"`
}

// wasmMatchOutput is the result from match_watchers. Read is what a watcher
// asking `over` needs before it is answered: the definitions of these types.
type wasmMatchOutput struct {
	MatchedIDs []string `json:"matched_ids"`
	Read       []struct {
		Types []string `json:"types"`
	} `json:"read"`
	Error string `json:"error,omitempty"`
}

// listOf is xs as a list Rust reads: a Go slice holding nothing is written
// null, and match_watchers reads a list, so it goes as the empty list it is.
func listOf[T any](xs []T) []T {
	return append(make([]T, 0, len(xs)), xs...)
}

// matchAttestationOf is an attestation as match_watchers reads it.
func matchAttestationOf(as *types.As) wasmMatchAttestation {
	return wasmMatchAttestation{
		Subjects:    listOf(as.Subjects),
		Predicates:  listOf(as.Predicates),
		Contexts:    listOf(as.Contexts),
		Actors:      listOf(as.Actors),
		TimestampMs: as.Timestamp.UnixMilli(),
		Attributes:  as.Attributes,
	}
}

// batchMatchStructural matches an attestation against all structural watchers
// in Rust, and returns the set of matched watcher IDs, and why each watcher it
// could not ask about was refused. A watcher asking `over` is measured by the
// definitions of the types the attestation is of, read through read when Rust
// asks for them.
func batchMatchStructural(as *types.As, watchers []*storage.Watcher, read typesReader) (map[string]bool, map[string]error, error) {
	engine, err := wasm.GetEngine()
	if err != nil {
		return nil, nil, errors.Wrapf(err, "wasm engine init, matching attestation %s", as.ID)
	}

	refused := make(map[string]error)
	filters := make([]wasmWatcherFilter, 0, len(watchers))
	for _, w := range watchers {
		f := wasmWatcherFilter{
			ID:               w.ID,
			Subjects:         listOf(w.Filter.Subjects),
			Predicates:       listOf(w.Filter.Predicates),
			Contexts:         listOf(w.Filter.Contexts),
			Actors:           listOf(w.Filter.Actors),
			AttributeFilters: listOf(w.AttributeFilters),
			Over:             w.Filter.Over,
		}
		start, starts, err := w.Filter.TimeStart.At("start")
		if err != nil {
			refused[w.ID] = errors.Wrapf(err, "watcher %s does not say its time range", w.ID)
			continue
		}
		end, ends, err := w.Filter.TimeEnd.At("end")
		if err != nil {
			refused[w.ID] = errors.Wrapf(err, "watcher %s does not say its time range", w.ID)
			continue
		}
		if starts {
			ms := start.UnixMilli()
			f.TimeStartMs = &ms
		}
		if ends {
			ms := end.UnixMilli()
			f.TimeEndMs = &ms
		}
		filters = append(filters, f)
	}

	input := wasmMatchInput{
		Attestation: matchAttestationOf(as),
		Watchers:    filters,
	}
	output, err := matchWatchers(engine, as.ID, input)
	if err != nil {
		return nil, nil, err
	}
	// Rust asks once, and the call that gives the types is answered whole.
	for _, asked := range output.Read {
		definitions, err := read(asked.Types)
		if err != nil {
			return nil, nil, errors.Wrapf(err, "failed to read the types attestation %s is of to measure it by", as.ID)
		}
		given := make([]wasmMatchAttestation, 0, len(definitions))
		for _, d := range definitions {
			given = append(given, matchAttestationOf(d))
		}
		input.Types = &given
		output, err = matchWatchers(engine, as.ID, input)
		if err != nil {
			return nil, nil, err
		}
	}

	matched := make(map[string]bool, len(output.MatchedIDs))
	for _, id := range output.MatchedIDs {
		matched[id] = true
	}
	return matched, refused, nil
}

// matchWatchers is one match_watchers call.
func matchWatchers(engine *wasm.Engine, id string, input wasmMatchInput) (wasmMatchOutput, error) {
	var output wasmMatchOutput
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return output, errors.Wrapf(err, "failed to marshal match_watchers input for attestation %s", id)
	}
	resultJSON, err := engine.Call("match_watchers", string(inputJSON))
	if err != nil {
		return output, errors.Wrapf(err, "match_watchers for attestation %s", id)
	}
	if err := json.Unmarshal([]byte(resultJSON), &output); err != nil {
		return output, errors.Wrapf(err, "match_watchers answered attestation %s with %s", id, resultJSON)
	}
	if output.Error != "" {
		return output, errors.Newf("match_watchers refused attestation %s: %s", id, output.Error)
	}
	return output, nil
}
