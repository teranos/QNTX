//go:build qntxwasm && !kern

package parser

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/wasm"
	"github.com/teranos/QNTX/errors"
)

// parseAxQueryDispatch uses the WASM-compiled qntx-core parser with temporal resolution.
func parseAxQueryDispatch(args []string, verbosity int, ctx ErrorContext) (*types.AxFilter, error) {
	return parseAxQueryWasm(args)
}

// resolvedInput is the JSON input for parse_ax_query_resolved.
type resolvedInput struct {
	Query string `json:"query"`
	NowMs int64  `json:"now_ms"`
}

// resolvedOutput is the JSON output from parse_ax_query_resolved.
type resolvedOutput struct {
	Subjects   []string         `json:"subjects"`
	Predicates []string         `json:"predicates"`
	Contexts   []string         `json:"contexts"`
	Actors     []string         `json:"actors"`
	Temporal   *resolvedTimeral `json:"temporal,omitempty"`
	Actions    []string         `json:"actions"`
	Error      string           `json:"error,omitempty"`
}

// resolvedTimeral represents resolved temporal clauses from Rust.
type resolvedTimeral struct {
	Since   *int64              `json:"Since,omitempty"`
	Until   *int64              `json:"Until,omitempty"`
	On      *resolvedOnClause   `json:"On,omitempty"`
	Between *resolvedBetween    `json:"Between,omitempty"`
	Over    *resolvedOverClause `json:"Over,omitempty"`
}

type resolvedOnClause struct {
	StartMs int64 `json:"start_ms"`
	EndMs   int64 `json:"end_ms"`
}

type resolvedBetween struct {
	StartMs int64 `json:"start_ms"`
	EndMs   int64 `json:"end_ms"`
}

type resolvedOverClause struct {
	Raw   string   `json:"raw"`
	Value *float64 `json:"value"`
	Unit  *string  `json:"unit"`
}

func parseAxQueryWasm(args []string) (*types.AxFilter, error) {
	engine, err := wasm.GetEngine()
	if err != nil {
		return nil, errors.Wrap(err, "wasm engine init")
	}

	input := resolvedInput{
		Query: strings.Join(args, " "),
		NowMs: time.Now().UnixMilli(),
	}

	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil, errors.Wrap(err, "marshal input")
	}

	resultJSON, err := engine.Call("parse_ax_query_resolved", string(inputJSON))
	if err != nil {
		return nil, errors.Wrap(err, "wasm parse_ax_query_resolved")
	}

	var out resolvedOutput
	if err := json.Unmarshal([]byte(resultJSON), &out); err != nil {
		return nil, errors.Wrap(err, "unmarshal wasm result")
	}

	if out.Error != "" {
		return nil, errors.Newf("ax parse: %s", out.Error)
	}

	return convertResolvedOutput(&out)
}

// convertResolvedOutput maps the resolved WASM output to Go's AxFilter.
func convertResolvedOutput(out *resolvedOutput) (*types.AxFilter, error) {
	filter := &types.AxFilter{
		Limit:  100,
		Format: "table",
	}

	filter.Subjects = nilIfEmpty(uppercaseTokens(out.Subjects))
	filter.Predicates = nilIfEmpty(out.Predicates)
	filter.Contexts = nilIfEmpty(lowercaseTokens(out.Contexts))
	filter.Actors = nilIfEmpty(lowercaseTokens(out.Actors))
	filter.SoActions = nilIfEmpty(out.Actions)

	if out.Temporal != nil {
		if out.Temporal.Since != nil {
			t := time.UnixMilli(*out.Temporal.Since)
			filter.TimeStart = &t
		}
		if out.Temporal.Until != nil {
			t := time.UnixMilli(*out.Temporal.Until)
			filter.TimeEnd = &t
		}
		if out.Temporal.On != nil {
			start := time.UnixMilli(out.Temporal.On.StartMs)
			end := time.UnixMilli(out.Temporal.On.EndMs)
			filter.TimeStart = &start
			filter.TimeEnd = &end
		}
		if out.Temporal.Between != nil {
			start := time.UnixMilli(out.Temporal.Between.StartMs)
			end := time.UnixMilli(out.Temporal.Between.EndMs)
			filter.TimeStart = &start
			filter.TimeEnd = &end
		}
	}

	return filter, nil
}
