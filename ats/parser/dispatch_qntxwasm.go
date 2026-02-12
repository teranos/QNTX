//go:build qntxwasm

package parser

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/wasm"
	"github.com/teranos/QNTX/errors"
)

// rustAxQuery mirrors the JSON output from qntx-core's Parser.
// Field names match the Rust serde serialization of AxQuery.
type rustAxQuery struct {
	Subjects   []string            `json:"subjects"`
	Predicates []string            `json:"predicates"`
	Contexts   []string            `json:"contexts"`
	Actors     []string            `json:"actors"`
	Temporal   *rustTemporalClause `json:"temporal"`
	Actions    []string            `json:"actions"`
	Error      string              `json:"error,omitempty"`
}

// rustTemporalClause handles the Rust enum serialization.
// Serde serializes enums as {"VariantName": value}.
type rustTemporalClause struct {
	Since   *string           `json:"Since,omitempty"`
	Until   *string           `json:"Until,omitempty"`
	On      *string           `json:"On,omitempty"`
	Between *[2]string        `json:"Between,omitempty"`
	Over    *rustDurationExpr `json:"Over,omitempty"`
}

type rustDurationExpr struct {
	Raw   string   `json:"raw"`
	Value *float64 `json:"value"`
	Unit  *string  `json:"unit"`
}

// parseAxQueryDispatch uses the WASM-compiled qntx-core parser exclusively.
// The Go parser fallback has been disabled and is being phased out.
func parseAxQueryDispatch(args []string, verbosity int, ctx ErrorContext) (*types.AxFilter, error) {
	return parseAxQueryWasm(args)
}

func parseAxQueryWasm(args []string) (*types.AxFilter, error) {
	engine, err := wasm.GetEngine()
	if err != nil {
		return nil, errors.Wrap(err, "wasm engine init")
	}

	// Join args into single string (Rust parser tokenizes internally)
	input := strings.Join(args, " ")

	resultJSON, err := engine.Call("parse_ax_query", input)
	if err != nil {
		return nil, errors.Wrap(err, "wasm parse_ax_query")
	}

	// Deserialize the Rust AxQuery
	var rq rustAxQuery
	if err := json.Unmarshal([]byte(resultJSON), &rq); err != nil {
		return nil, errors.Wrap(err, "unmarshal wasm result")
	}

	// Check for parser error from Rust
	if rq.Error != "" {
		return nil, errors.Newf("ax parse: %s", rq.Error)
	}

	// Convert Rust AxQuery → Go AxFilter
	return convertRustQuery(&rq)
}

// nilIfEmpty returns nil for empty slices to match Go parser behavior.
// Rust's serde serializes empty Vec<String> as [], which unmarshals to
// []string{} in Go, but the Go parser returns nil for empty fields.
func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

// convertRustQuery maps the Rust parser output to Go's AxFilter,
// applying case normalization and temporal resolution.
func convertRustQuery(rq *rustAxQuery) (*types.AxFilter, error) {
	filter := &types.AxFilter{
		Limit:  100,
		Format: "table",
	}

	// Case normalization matching Go parser behavior
	// Rust's serde serializes empty Vec<String> as [], which unmarshals to
	// []string{} in Go, but the Go parser returns nil for empty fields.
	// We apply nilIfEmpty after case transformations to ensure consistency.
	filter.Subjects = nilIfEmpty(uppercaseTokens(rq.Subjects))
	filter.Predicates = nilIfEmpty(rq.Predicates)
	filter.Contexts = nilIfEmpty(lowercaseTokens(rq.Contexts))
	filter.Actors = nilIfEmpty(lowercaseTokens(rq.Actors))
	filter.SoActions = nilIfEmpty(rq.Actions)

	// Resolve temporal expressions
	if rq.Temporal != nil {
		if err := resolveTemporalClause(rq.Temporal, filter); err != nil {
			// Return the temporal parsing error with context.
			// The Go parser returns structured errors for temporal failures,
			// so the WASM parser should maintain consistency.
			return nil, errors.Wrap(err, "failed to parse temporal expression")
		}
	}

	return filter, nil
}

// resolveTemporalClause converts Rust temporal strings into Go time.Time values.
// Delegates to the existing Go temporal parser which handles ISO dates, relative
// expressions, and named days.
func resolveTemporalClause(tc *rustTemporalClause, filter *types.AxFilter) error {
	if tc.Since != nil {
		t, err := ParseTemporalExpression(*tc.Since)
		if err != nil {
			return errors.Wrapf(err, "invalid 'since' expression: %s", *tc.Since)
		}
		filter.TimeStart = t
	}

	if tc.Until != nil {
		t, err := ParseTemporalExpression(*tc.Until)
		if err != nil {
			return errors.Wrapf(err, "invalid 'until' expression: %s", *tc.Until)
		}
		filter.TimeEnd = t
	}

	if tc.On != nil {
		t, err := ParseTemporalExpression(*tc.On)
		if err != nil {
			return errors.Wrapf(err, "invalid 'on' expression: %s", *tc.On)
		}
		startOfDay := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
		endOfDay := startOfDay.Add(24 * time.Hour)
		filter.TimeStart = &startOfDay
		filter.TimeEnd = &endOfDay
	}

	if tc.Between != nil {
		tStart, err := ParseTemporalExpression(tc.Between[0])
		if err != nil {
			return err
		}
		tEnd, err := ParseTemporalExpression(tc.Between[1])
		if err != nil {
			return err
		}
		filter.TimeStart = tStart
		filter.TimeEnd = tEnd
	}

	if tc.Over != nil {
		filter.OverComparison = &types.OverFilter{
			Operator: "over",
		}
		if tc.Over.Value != nil {
			filter.OverComparison.Value = *tc.Over.Value
		}
		if tc.Over.Unit != nil {
			// Rust uses "Years"/"Months"/etc, Go uses "y"/"m"
			filter.OverComparison.Unit = rustUnitToGo(*tc.Over.Unit)
		}
	}

	return nil
}

func rustUnitToGo(unit string) string {
	switch unit {
	case "Years":
		return "y"
	case "Months":
		return "m"
	case "Weeks":
		return "w"
	case "Days":
		return "d"
	default:
		return unit
	}
}
