//go:build qntxwasm

package parser

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/wasm"
	errors "github.com/teranos/sacred-error"
)

// parseAxQueryDispatch uses the WASM-compiled ats parser with temporal resolution.
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
		NowMs: timeNow().UnixMilli(),
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
		// A query naming no time is unbounded on both sides.
		TimeStart: types.Unbounded(),
		TimeEnd:   types.Unbounded(),
	}

	filter.Subjects = nilIfEmpty(uppercaseTokens(out.Subjects))
	filter.Predicates = nilIfEmpty(out.Predicates)
	filter.Contexts = nilIfEmpty(lowercaseTokens(out.Contexts))
	filter.Actors = nilIfEmpty(lowercaseTokens(out.Actors))
	filter.SoActions = nilIfEmpty(out.Actions)

	if out.Temporal != nil {
		if out.Temporal.Since != nil {
			t := time.UnixMilli(*out.Temporal.Since)
			filter.TimeStart = types.BoundAt(t)
		}
		if out.Temporal.Until != nil {
			t := time.UnixMilli(*out.Temporal.Until)
			filter.TimeEnd = types.BoundAt(t)
		}
		if out.Temporal.On != nil {
			start := time.UnixMilli(out.Temporal.On.StartMs)
			end := time.UnixMilli(out.Temporal.On.EndMs)
			filter.TimeStart = types.BoundAt(start)
			filter.TimeEnd = types.BoundAt(end)
		}
		if out.Temporal.Between != nil {
			start := time.UnixMilli(out.Temporal.Between.StartMs)
			end := time.UnixMilli(out.Temporal.Between.EndMs)
			filter.TimeStart = types.BoundAt(start)
			filter.TimeEnd = types.BoundAt(end)
		}
	}

	return filter, nil
}

// temporalInput is the JSON input for resolve_temporal_expr.
type temporalInput struct {
	Expr  string `json:"expr"`
	NowMs int64  `json:"now_ms"`
	Span  string `json:"span"`
}

// answer reads an export's answer into into, or why it refused when it names
// an error.
func answer(fnName, resultJSON string, into any) error {
	var out map[string]json.RawMessage
	if err := json.Unmarshal([]byte(resultJSON), &out); err != nil {
		return errors.Wrapf(err, "unmarshal %s result %s", fnName, resultJSON)
	}
	if why, refused := out["error"]; refused {
		var says string
		if err := json.Unmarshal(why, &says); err != nil {
			return errors.Wrapf(err, "%s refused with %s", fnName, why)
		}
		return errors.New(says)
	}
	if err := json.Unmarshal([]byte(resultJSON), into); err != nil {
		return errors.Wrapf(err, "unmarshal %s result %s", fnName, resultJSON)
	}
	return nil
}

// resolveTemporalWasm resolves expr as span ("moment" or "day") and answers
// with the value under the key the span names ("ms" or "day").
func resolveTemporalWasm(expr string, now time.Time, span, key string, into any) error {
	engine, err := wasm.GetEngine()
	if err != nil {
		return errors.Wrap(err, "wasm engine init")
	}

	inputJSON, err := json.Marshal(temporalInput{Expr: expr, NowMs: now.UnixMilli(), Span: span})
	if err != nil {
		return errors.Wrapf(err, "marshal temporal input %q", expr)
	}

	resultJSON, err := engine.Call("resolve_temporal_expr", string(inputJSON))
	if err != nil {
		return errors.Wrapf(err, "wasm resolve_temporal_expr %q", expr)
	}

	var out map[string]json.RawMessage
	if err := answer("resolve_temporal_expr", resultJSON, &out); err != nil {
		return err
	}
	value, answered := out[key]
	if !answered {
		return errors.Newf("resolve_temporal_expr answered %q with %s, which names no %s", expr, resultJSON, key)
	}
	if err := json.Unmarshal(value, into); err != nil {
		return errors.Wrapf(err, "resolve_temporal_expr answered %q with %s", expr, resultJSON)
	}
	return nil
}

// resolveTemporalDispatch resolves one expression through the Rust resolver.
func resolveTemporalDispatch(expr string, now time.Time) (int64, error) {
	var ms int64
	if err := resolveTemporalWasm(expr, now, "moment", "ms", &ms); err != nil {
		return 0, err
	}
	return ms, nil
}

// resolveTemporalDayDispatch resolves the UTC day an expression falls on.
func resolveTemporalDayDispatch(expr string, now time.Time) (int64, int64, error) {
	var day resolvedOnClause
	if err := resolveTemporalWasm(expr, now, "day", "day", &day); err != nil {
		return 0, 0, err
	}
	return day.StartMs, day.EndMs, nil
}

// asInput is the JSON input for parse_as_command.
type asInput struct {
	Args  []string `json:"args"`
	NowMs int64    `json:"now_ms"`
}

// parseAsCommandDispatch parses an AS command through the Rust parser.
func parseAsCommandDispatch(args []string, now time.Time) (*asParsed, error) {
	engine, err := wasm.GetEngine()
	if err != nil {
		return nil, errors.Wrap(err, "wasm engine init")
	}

	inputJSON, err := json.Marshal(asInput{Args: args, NowMs: now.UnixMilli()})
	if err != nil {
		return nil, errors.Wrapf(err, "marshal as input %v", args)
	}

	resultJSON, err := engine.Call("parse_as_command", string(inputJSON))
	if err != nil {
		return nil, errors.Wrapf(err, "wasm parse_as_command %v", args)
	}

	var parsed asParsed
	if err := answer("parse_as_command", resultJSON, &parsed); err != nil {
		return nil, err
	}
	return &parsed, nil
}
