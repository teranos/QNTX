//go:build cgo

// Package parser provides AX query parsing functionality.
// This file provides the CGO implementation using the Rust parser.

package parser

import (
	"strings"

	"github.com/teranos/QNTX/ats/ax/qntx-parser/axparser"
	"github.com/teranos/QNTX/ats/types"
)

// RustParserAvailable indicates whether the Rust parser is available.
// This is true when built with CGO enabled.
const RustParserAvailable = true

// parseAxQueryImpl parses using the Rust parser via CGO.
func parseAxQueryImpl(query string) (*types.AxFilter, error) {
	return parseAxQueryWithVerbosityImpl(query, 0)
}

// parseAxQueryWithVerbosityImpl parses using the Rust parser with verbosity.
func parseAxQueryWithVerbosityImpl(query string, verbosity int) (*types.AxFilter, error) {
	result, err := axparser.Parse(query)
	if err != nil {
		// Fall back to Go parser on error for better error messages
		args := strings.Fields(query)
		return parseAxQuery(args, verbosity, ErrorContextPlain)
	}

	filter := &types.AxFilter{
		Limit:  100,
		Format: "table",
	}

	// Convert Rust result to Go AxFilter
	filter.Subjects = normalizeSubjects(result.Subjects)
	filter.Predicates = result.Predicates
	filter.Contexts = normalizeContexts(result.Contexts)
	filter.Actors = normalizeActors(result.Actors)
	filter.SoActions = result.Actions

	// Convert temporal clause
	if result.Temporal != nil {
		err := parseTemporalToFilter(
			int(result.Temporal.Type),
			result.Temporal.Start,
			result.Temporal.End,
			result.Temporal.DurationValue,
			int(result.Temporal.DurationUnit),
			result.Temporal.DurationRaw,
			filter,
		)
		if err != nil {
			// Log warning but continue - temporal parsing can be lenient
			// In production, we'd use a proper logger here
		}
	}

	return filter, nil
}

// normalizeSubjects converts subjects to uppercase for consistent storage.
func normalizeSubjects(subjects []string) []string {
	if len(subjects) == 0 {
		return nil
	}
	result := make([]string, len(subjects))
	for i, s := range subjects {
		result[i] = strings.ToUpper(s)
	}
	return result
}

// normalizeContexts converts contexts to lowercase for consistency.
func normalizeContexts(contexts []string) []string {
	if len(contexts) == 0 {
		return nil
	}
	result := make([]string, len(contexts))
	for i, c := range contexts {
		result[i] = strings.ToLower(c)
	}
	return result
}

// normalizeActors converts actors to lowercase for consistency.
func normalizeActors(actors []string) []string {
	if len(actors) == 0 {
		return nil
	}
	result := make([]string, len(actors))
	for i, a := range actors {
		result[i] = strings.ToLower(a)
	}
	return result
}
