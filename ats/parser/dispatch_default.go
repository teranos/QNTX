//go:build !qntxwasm

package parser

import "github.com/teranos/QNTX/ats/types"

// parseAxQueryDispatch uses the pure Go parser when the qntxwasm build tag
// is not set.
func parseAxQueryDispatch(args []string, verbosity int, ctx ErrorContext) (*types.AxFilter, error) {
	return parseAxQueryGo(args, verbosity, ctx)
}
