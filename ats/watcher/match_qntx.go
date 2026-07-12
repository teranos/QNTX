//go:build qntxwasm

package watcher

import (
	"encoding/json"

	"github.com/teranos/QNTX/ats/storage"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/wasm"
)

// wasmMatchAttestation is the JSON shape Rust expects.
type wasmMatchAttestation struct {
	Subjects    []string               `json:"subjects"`
	Predicates  []string               `json:"predicates"`
	Contexts    []string               `json:"contexts"`
	Actors      []string               `json:"actors"`
	TimestampMs int64                  `json:"timestamp_ms"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
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
}

// wasmMatchInput is the full input for match_watchers.
type wasmMatchInput struct {
	Attestation wasmMatchAttestation `json:"attestation"`
	Watchers    []wasmWatcherFilter  `json:"watchers"`
}

// wasmMatchOutput is the result from match_watchers.
type wasmMatchOutput struct {
	MatchedIDs []string `json:"matched_ids"`
	Error      string   `json:"error,omitempty"`
}

// batchMatchStructural calls Rust WASM to match an attestation against all structural watchers.
// Falls back to Go logic if the WASM engine is unavailable.
// Returns the set of matched watcher IDs.
func batchMatchStructural(as *types.As, watchers []*storage.Watcher) map[string]bool {
	engine, err := wasm.GetEngine()
	if err != nil {
		return batchMatchStructuralGo(as, watchers)
	}

	// Build input
	att := wasmMatchAttestation{
		Subjects:    as.Subjects,
		Predicates:  as.Predicates,
		Contexts:    as.Contexts,
		Actors:      as.Actors,
		TimestampMs: as.Timestamp.UnixMilli(),
		Attributes:  as.Attributes,
	}

	filters := make([]wasmWatcherFilter, 0, len(watchers))
	for _, w := range watchers {
		f := wasmWatcherFilter{
			ID:               w.ID,
			Subjects:         w.Filter.Subjects,
			Predicates:       w.Filter.Predicates,
			Contexts:         w.Filter.Contexts,
			Actors:           w.Filter.Actors,
			AttributeFilters: w.AttributeFilters,
		}
		if w.Filter.TimeStart != nil {
			ms := w.Filter.TimeStart.UnixMilli()
			f.TimeStartMs = &ms
		}
		if w.Filter.TimeEnd != nil {
			ms := w.Filter.TimeEnd.UnixMilli()
			f.TimeEndMs = &ms
		}
		filters = append(filters, f)
	}

	input := wasmMatchInput{
		Attestation: att,
		Watchers:    filters,
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return batchMatchStructuralGo(as, watchers)
	}

	resultJSON, err := engine.Call("match_watchers", string(inputJSON))
	if err != nil {
		return batchMatchStructuralGo(as, watchers)
	}

	var output wasmMatchOutput
	if err := json.Unmarshal([]byte(resultJSON), &output); err != nil {
		return batchMatchStructuralGo(as, watchers)
	}

	if output.Error != "" {
		return batchMatchStructuralGo(as, watchers)
	}

	matched := make(map[string]bool, len(output.MatchedIDs))
	for _, id := range output.MatchedIDs {
		matched[id] = true
	}
	return matched
}
