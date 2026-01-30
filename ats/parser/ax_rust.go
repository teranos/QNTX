// Package parser provides AX query parsing functionality.
//
// This file provides the bridge between the Rust AX parser and the Go types.
// The actual implementation is split between ax_rust_cgo.go (CGO enabled)
// and ax_rust_nocgo.go (pure Go fallback).

package parser

import (
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

// ParseAxQuery parses an AX query string and returns an AxFilter.
// This is the main entry point that chooses between Rust (CGO) and Go parsers.
//
// Unlike ParseAxCommand which takes CLI args, this takes a single query string.
// Use this for programmatic parsing where the query is already a string.
//
// Example:
//
//	filter, err := ParseAxQuery("ALICE is author of GitHub since 2024-01-01")
func ParseAxQuery(query string) (*types.AxFilter, error) {
	return parseAxQueryImpl(query)
}

// ParseAxQueryWithVerbosity parses an AX query with verbosity level for error context.
func ParseAxQueryWithVerbosity(query string, verbosity int) (*types.AxFilter, error) {
	return parseAxQueryWithVerbosityImpl(query, verbosity)
}

// convertArgsToQuery converts CLI args to a query string suitable for parsing.
// Handles quoting of multi-word arguments.
func convertArgsToQuery(args []string) string {
	var parts []string
	for _, arg := range args {
		// If arg contains spaces and isn't already quoted, quote it
		if strings.Contains(arg, " ") && !strings.HasPrefix(arg, "'") {
			parts = append(parts, "'"+arg+"'")
		} else {
			parts = append(parts, arg)
		}
	}
	return strings.Join(parts, " ")
}

// parseTemporalToFilter converts temporal clause data to AxFilter time fields.
// Used by both CGO and nocgo implementations.
func parseTemporalToFilter(temporalType int, start, end string, durationValue float64, durationUnit int, durationRaw string, filter *types.AxFilter) error {
	switch temporalType {
	case 1: // Since
		if start != "" {
			t, err := ParseTemporalExpression(start)
			if err != nil {
				return err
			}
			filter.TimeStart = t
		}
	case 2: // Until
		if start != "" {
			t, err := ParseTemporalExpression(start)
			if err != nil {
				return err
			}
			filter.TimeEnd = t
		}
	case 3: // On
		if start != "" {
			t, err := ParseTemporalExpression(start)
			if err != nil {
				return err
			}
			// "on" means both start and end of that day
			startOfDay := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
			endOfDay := startOfDay.Add(24 * time.Hour)
			filter.TimeStart = &startOfDay
			filter.TimeEnd = &endOfDay
		}
	case 4: // Between
		if start != "" {
			t, err := ParseTemporalExpression(start)
			if err != nil {
				return err
			}
			filter.TimeStart = t
		}
		if end != "" {
			t, err := ParseTemporalExpression(end)
			if err != nil {
				return err
			}
			filter.TimeEnd = t
		}
	case 5: // Over
		if durationValue > 0 && durationRaw != "" {
			unit := "y" // default to years
			switch durationUnit {
			case 1:
				unit = "y"
			case 2:
				unit = "m"
			case 3:
				unit = "w"
			case 4:
				unit = "d"
			}
			filter.OverComparison = &types.OverFilter{
				Value:    durationValue,
				Unit:     unit,
				Operator: "over",
			}
		}
	}
	return nil
}

// tokenizeQuery splits a query string into tokens, respecting quoted strings.
// Used by both CGO fallback and nocgo implementations.
func tokenizeQuery(query string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false

	for i := 0; i < len(query); i++ {
		c := query[i]

		if c == '\'' {
			if inQuote {
				// End of quoted string
				current.WriteByte(c)
				tokens = append(tokens, current.String())
				current.Reset()
				inQuote = false
			} else {
				// Start of quoted string
				if current.Len() > 0 {
					tokens = append(tokens, current.String())
					current.Reset()
				}
				current.WriteByte(c)
				inQuote = true
			}
		} else if c == ' ' && !inQuote {
			// Space outside quotes - token boundary
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		} else {
			current.WriteByte(c)
		}
	}

	// Don't forget the last token
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}
