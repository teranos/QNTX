//go:build !cgo

// Package parser provides AX query parsing functionality.
// This file provides the pure Go fallback when CGO is disabled.

package parser

import (
	"github.com/teranos/QNTX/ats/types"
)

// RustParserAvailable indicates whether the Rust parser is available.
// This is false when built without CGO.
const RustParserAvailable = false

// parseAxQueryImpl uses the Go parser when CGO is not available.
func parseAxQueryImpl(query string) (*types.AxFilter, error) {
	return parseAxQueryWithVerbosityImpl(query, 0)
}

// parseAxQueryWithVerbosityImpl uses the Go parser when CGO is not available.
func parseAxQueryWithVerbosityImpl(query string, verbosity int) (*types.AxFilter, error) {
	// Convert query string to args for the Go parser
	args := tokenizeQuery(query)
	return parseAxQuery(args, verbosity, ErrorContextPlain)
}
