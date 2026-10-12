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

// axFilterInput is the JSON input for ax_filter.
type axFilterInput struct {
	Query string `json:"query"`
	NowMs int64  `json:"now_ms"`
	Limit int    `json:"limit"`
}

// axFilterOutput is ax_filter's answer: the filter the query names, and its
// `so` actions. The browser reads queries with the same function.
type axFilterOutput struct {
	Filter struct {
		Subjects   []string        `json:"subjects"`
		Predicates []string        `json:"predicates"`
		Contexts   []string        `json:"contexts"`
		Actors     []string        `json:"actors"`
		TimeStart  json.RawMessage `json:"time_start"`
		TimeEnd    json.RawMessage `json:"time_end"`
		Limit      int             `json:"limit"`
		Over       *string         `json:"over"`
	} `json:"filter"`
	Actions []string `json:"actions"`
}

// resolvedOnClause is a UTC day span: start inclusive, end exclusive.
type resolvedOnClause struct {
	StartMs int64 `json:"start_ms"`
	EndMs   int64 `json:"end_ms"`
}

// axLimit is how many rows a CLI ax query reads.
const axLimit = 100

func parseAxQueryWasm(args []string) (*types.AxFilter, error) {
	return readAxQueryWasm("ax_filter", args)
}

// parseAxWatcherDispatch reads a watcher's query through watch_filter, which
// refuses `over`: a watcher matches each attestation as it arrives and reads no
// type definitions to measure it with.
func parseAxWatcherDispatch(args []string) (*types.AxFilter, error) {
	return readAxQueryWasm("watch_filter", args)
}

// readAxQueryWasm reads a query through fnName, ax_filter or watch_filter.
func readAxQueryWasm(fnName string, args []string) (*types.AxFilter, error) {
	query := strings.Join(args, " ")

	engine, err := wasm.GetEngine()
	if err != nil {
		return nil, errors.Wrap(err, "wasm engine init")
	}

	inputJSON, err := json.Marshal(axFilterInput{Query: query, NowMs: timeNow().UnixMilli(), Limit: axLimit})
	if err != nil {
		return nil, errors.Wrapf(err, "marshal ax_filter input %q", query)
	}

	resultJSON, err := engine.Call(fnName, string(inputJSON))
	if err != nil {
		return nil, errors.Wrapf(err, "wasm %s %q", fnName, query)
	}

	var out axFilterOutput
	if err := answer(fnName, resultJSON, &out); err != nil {
		return nil, errors.Wrapf(err, "ax parse %q", query)
	}

	start, err := timeBoundOf("time_start", out.Filter.TimeStart)
	if err != nil {
		return nil, errors.Wrapf(err, "%s answered %q with %s", fnName, query, resultJSON)
	}
	end, err := timeBoundOf("time_end", out.Filter.TimeEnd)
	if err != nil {
		return nil, errors.Wrapf(err, "%s answered %q with %s", fnName, query, resultJSON)
	}

	// The node's stores match subjects upper case and contexts and actors
	// lower; the browser's store matches as written, so the folding is Go's.
	return &types.AxFilter{
		Subjects:   nilIfEmpty(uppercaseTokens(out.Filter.Subjects)),
		Predicates: nilIfEmpty(out.Filter.Predicates),
		Contexts:   nilIfEmpty(lowercaseTokens(out.Filter.Contexts)),
		Actors:     nilIfEmpty(lowercaseTokens(out.Filter.Actors)),
		SoActions:  nilIfEmpty(out.Actions),
		TimeStart:  start,
		TimeEnd:    end,
		Limit:      out.Filter.Limit,
		Format:     "table",
		Over:       out.Filter.Over,
	}, nil
}

// timeBoundOf reads one side of ax_filter's range: epoch milliseconds, or
// null where the query names no time on that side, which is unbounded. A side
// the answer leaves out is refused.
func timeBoundOf(side string, raw json.RawMessage) (types.TimeBound, error) {
	if string(raw) == "null" {
		return types.Unbounded(), nil
	}
	var ms int64
	if err := json.Unmarshal(raw, &ms); err != nil {
		return types.TimeBound{}, errors.Wrapf(err, "%s is %s, neither epoch milliseconds nor null", side, raw)
	}
	return types.BoundAt(timeAtMs(ms)), nil
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
