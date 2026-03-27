//go:build qntxwasm && !kern

package parser

import (
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

func parseAxQueryWasm(args []string) (*types.AxFilter, error) {
	engine, err := wasm.GetEngine()
	if err != nil {
		return nil, errors.Wrap(err, "wasm engine init")
	}

	input := strings.Join(args, " ")
	nowMs := time.Now().UnixMilli()

	output, err := engine.ParseAxQueryResolved(input, nowMs)
	if err != nil {
		return nil, errors.Wrap(err, "wasm parse_ax_query_resolved")
	}

	return convertResolvedQuery(output)
}

// convertResolvedQuery maps the resolved parser output (with absolute timestamps)
// to Go's AxFilter. Temporal expressions are already resolved to milliseconds by Rust.
func convertResolvedQuery(rq *wasm.ParseResolvedOutput) (*types.AxFilter, error) {
	filter := &types.AxFilter{
		Limit:  100,
		Format: "table",
	}

	filter.Subjects = nilIfEmpty(uppercaseTokens(rq.Subjects))
	filter.Predicates = nilIfEmpty(rq.Predicates)
	filter.Contexts = nilIfEmpty(lowercaseTokens(rq.Contexts))
	filter.Actors = nilIfEmpty(lowercaseTokens(rq.Actors))
	filter.SoActions = nilIfEmpty(rq.Actions)

	if rq.Temporal != nil {
		applyResolvedTemporal(rq.Temporal, filter)
	}

	return filter, nil
}

// applyResolvedTemporal maps resolved millisecond timestamps to Go time.Time values.
func applyResolvedTemporal(rt *wasm.ResolvedTemporalOutput, filter *types.AxFilter) {
	switch rt.Type {
	case "Since":
		if rt.SinceMs != nil {
			t := time.UnixMilli(*rt.SinceMs)
			filter.TimeStart = &t
		}
	case "Until":
		if rt.UntilMs != nil {
			t := time.UnixMilli(*rt.UntilMs)
			filter.TimeEnd = &t
		}
	case "On":
		if rt.StartMs != nil {
			start := time.UnixMilli(*rt.StartMs)
			filter.TimeStart = &start
		}
		if rt.EndMs != nil {
			end := time.UnixMilli(*rt.EndMs)
			filter.TimeEnd = &end
		}
	case "Between":
		if rt.StartMs != nil {
			start := time.UnixMilli(*rt.StartMs)
			filter.TimeStart = &start
		}
		if rt.EndMs != nil {
			end := time.UnixMilli(*rt.EndMs)
			filter.TimeEnd = &end
		}
	case "Over":
		filter.OverComparison = &types.OverFilter{
			Value:    rt.Value,
			Unit:     rt.Unit,
			Operator: "over",
		}
	}
}
