//go:build !qntxwasm

package parser

import (
	"time"

	"github.com/teranos/QNTX/ats/types"
	errors "github.com/teranos/sacred-error"
)

// rebuild says what a build without the Rust parser is missing.
const rebuild = "requires qntxwasm build tag - rebuild with 'make cli' or 'go build -tags qntxwasm'"

// parseAxQueryDispatch returns an error when WASM parser is not available.
func parseAxQueryDispatch(args []string, verbosity int, ctx ErrorContext) (*types.AxFilter, error) {
	return nil, errors.New("ax query parsing " + rebuild)
}

// resolveTemporalDispatch returns an error when WASM parser is not available.
func resolveTemporalDispatch(expr string, now time.Time) (int64, error) {
	return 0, errors.Newf("resolving %q %s", expr, rebuild)
}

// resolveTemporalDayDispatch returns an error when WASM parser is not available.
func resolveTemporalDayDispatch(expr string, now time.Time) (int64, int64, error) {
	return 0, 0, errors.Newf("resolving the day of %q %s", expr, rebuild)
}

// parseAsCommandDispatch returns an error when WASM parser is not available.
func parseAsCommandDispatch(args []string, now time.Time) (*asParsed, error) {
	return nil, errors.Newf("as command parsing of %v %s", args, rebuild)
}
