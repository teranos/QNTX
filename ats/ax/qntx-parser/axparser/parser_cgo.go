// Package axparser provides a CGO wrapper for the Rust AX query parser.
//
// This package links directly with the Rust library via CGO, providing
// high-performance parsing for ax (⋈) queries.
//
// Build Requirements:
//   - Rust toolchain (cargo build --release in ats/ax/qntx-parser)
//   - CGO enabled (CGO_ENABLED=1)
//   - Library path set correctly for your platform
//
// Usage:
//
//	result, err := axparser.Parse("ALICE is author of GitHub")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(result.Subjects)  // ["ALICE"]
//	fmt.Println(result.Predicates) // ["author"]
//	fmt.Println(result.Contexts)   // ["GitHub"]
package axparser

/*
#cgo CFLAGS: -I${SRCDIR}/../include
#cgo linux LDFLAGS: -L${SRCDIR}/../../../../target/release -lqntx_parser -lpthread -ldl -lm
#cgo darwin LDFLAGS: -L${SRCDIR}/../../../../target/release -lqntx_parser -lpthread -ldl -lm
#cgo windows LDFLAGS: -L${SRCDIR}/../../../../target/release -lqntx_parser -lws2_32 -luserenv

#include "ax_parser.h"
#include <stdlib.h>
*/
import "C"

import (
	"errors"
	"time"
	"unsafe"
)

// TemporalType represents the type of temporal constraint
type TemporalType int

const (
	// TemporalNone indicates no temporal constraint
	TemporalNone TemporalType = 0
	// TemporalSince represents "since DATE"
	TemporalSince TemporalType = 1
	// TemporalUntil represents "until DATE"
	TemporalUntil TemporalType = 2
	// TemporalOn represents "on DATE"
	TemporalOn TemporalType = 3
	// TemporalBetween represents "between DATE and DATE"
	TemporalBetween TemporalType = 4
	// TemporalOver represents "over DURATION"
	TemporalOver TemporalType = 5
)

// DurationUnit represents the unit for duration expressions
type DurationUnit int

const (
	// DurationUnknown indicates unparsed or unknown unit
	DurationUnknown DurationUnit = 0
	// DurationYears represents years
	DurationYears DurationUnit = 1
	// DurationMonths represents months
	DurationMonths DurationUnit = 2
	// DurationWeeks represents weeks
	DurationWeeks DurationUnit = 3
	// DurationDays represents days
	DurationDays DurationUnit = 4
)

// TemporalClause represents a parsed temporal constraint
type TemporalClause struct {
	// Type of temporal constraint
	Type TemporalType
	// Start date/time (for Since, Until, On, Between)
	Start string
	// End date/time (for Between only)
	End string
	// Duration value (for Over only)
	DurationValue float64
	// Duration unit (for Over only)
	DurationUnit DurationUnit
	// Raw duration string (for Over only)
	DurationRaw string
}

// ParseResult represents a parsed AX query
type ParseResult struct {
	// Subject entities
	Subjects []string
	// Predicates (what is being queried)
	Predicates []string
	// Contexts (of/from clause)
	Contexts []string
	// Actors (by/via clause)
	Actors []string
	// Temporal constraint
	Temporal *TemporalClause
	// Actions (so/therefore clause)
	Actions []string
	// Parse time
	ParseTime time.Duration
}

// Parse parses an AX query string and returns the structured result.
//
// Example:
//
//	result, err := axparser.Parse("ALICE BOB is author_of of GitHub by CHARLIE since 2024-01-01 so notify")
//	if err != nil {
//	    return err
//	}
//	// result.Subjects = ["ALICE", "BOB"]
//	// result.Predicates = ["author_of"]
//	// result.Contexts = ["GitHub"]
//	// result.Actors = ["CHARLIE"]
//	// result.Temporal.Type = TemporalSince
//	// result.Temporal.Start = "2024-01-01"
//	// result.Actions = ["notify"]
func Parse(query string) (*ParseResult, error) {
	cQuery := C.CString(query)
	defer C.free(unsafe.Pointer(cQuery))

	result := C.parser_parse_query(cQuery)
	defer C.parser_result_free(result)

	if !result.success {
		errMsg := C.GoString(result.error_msg)
		return nil, errors.New(errMsg)
	}

	parsed := &ParseResult{
		Subjects:   cStringArrayToGo(result.subjects, result.subjects_len),
		Predicates: cStringArrayToGo(result.predicates, result.predicates_len),
		Contexts:   cStringArrayToGo(result.contexts, result.contexts_len),
		Actors:     cStringArrayToGo(result.actors, result.actors_len),
		Actions:    cStringArrayToGo(result.actions, result.actions_len),
		ParseTime:  time.Duration(result.parse_time_us) * time.Microsecond,
	}

	// Convert temporal clause if present
	if result.temporal.temporal_type != C.TEMPORAL_NONE {
		parsed.Temporal = &TemporalClause{
			Type:          TemporalType(result.temporal.temporal_type),
			Start:         C.GoString(result.temporal.start),
			End:           C.GoString(result.temporal.end),
			DurationValue: float64(result.temporal.duration_value),
			DurationUnit:  DurationUnit(result.temporal.duration_unit),
			DurationRaw:   C.GoString(result.temporal.duration_raw),
		}
	}

	return parsed, nil
}

// HasSubjects returns true if the query has subjects
func (p *ParseResult) HasSubjects() bool {
	return len(p.Subjects) > 0
}

// HasPredicates returns true if the query has predicates
func (p *ParseResult) HasPredicates() bool {
	return len(p.Predicates) > 0
}

// HasContexts returns true if the query has contexts
func (p *ParseResult) HasContexts() bool {
	return len(p.Contexts) > 0
}

// HasActors returns true if the query has actors
func (p *ParseResult) HasActors() bool {
	return len(p.Actors) > 0
}

// HasTemporal returns true if the query has a temporal clause
func (p *ParseResult) HasTemporal() bool {
	return p.Temporal != nil
}

// HasActions returns true if the query has actions
func (p *ParseResult) HasActions() bool {
	return len(p.Actions) > 0
}

// IsEmpty returns true if the query has no clauses
func (p *ParseResult) IsEmpty() bool {
	return !p.HasSubjects() && !p.HasPredicates() && !p.HasContexts() &&
		!p.HasActors() && !p.HasTemporal() && !p.HasActions()
}

// cStringArrayToGo converts a C string array to a Go string slice
func cStringArrayToGo(arr **C.char, length C.size_t) []string {
	if arr == nil || length == 0 {
		return nil
	}

	result := make([]string, int(length))
	cStrings := unsafe.Slice(arr, int(length))

	for i, cStr := range cStrings {
		result[i] = C.GoString(cStr)
	}

	return result
}
