//go:build !qntxwasm

package parser

import (
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
)

// parseAxQueryDispatch returns an error when WASM parser is not available.
func parseAxQueryDispatch(args []string, verbosity int, ctx ErrorContext) (*types.AxFilter, error) {
	return nil, errors.New("ax query parsing requires qntxwasm build tag - rebuild with 'make cli' or 'go build -tags qntxwasm'")
}
