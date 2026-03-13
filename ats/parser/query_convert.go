package parser

import (
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// rustAxQuery mirrors the JSON output from qntx-core's Parser (and kern).
// Field names match the serde/yojson serialization of AxQuery.
type rustAxQuery struct {
	Subjects   []string            `json:"subjects"`
	Predicates []string            `json:"predicates"`
	Contexts   []string            `json:"contexts"`
	Actors     []string            `json:"actors"`
	Temporal   *rustTemporalClause `json:"temporal"`
	Actions    []string            `json:"actions"`
	Error      string              `json:"error,omitempty"`
}

// rustTemporalClause handles the enum serialization.
// Serde/Yojson serializes enums as {"VariantName": value}.
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

// nilIfEmpty returns nil for empty slices to match Go parser behavior.
func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

// convertRustQuery maps the parser output to Go's AxFilter,
// applying case normalization and temporal resolution.
func convertRustQuery(rq *rustAxQuery) (*types.AxFilter, error) {
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
		if err := resolveTemporalClause(rq.Temporal, filter); err != nil {
			return nil, errors.Wrap(err, "failed to parse temporal expression")
		}
	}

	return filter, nil
}

// resolveTemporalClause converts temporal strings into Go time.Time values.
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
