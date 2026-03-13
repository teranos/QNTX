//go:build qntxwasm && !kern

package parser

import (
	"encoding/json"
	"strings"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/ats/wasm"
	"github.com/teranos/QNTX/errors"
)

// parseAxQueryDispatch uses the WASM-compiled qntx-core parser exclusively.
func parseAxQueryDispatch(args []string, verbosity int, ctx ErrorContext) (*types.AxFilter, error) {
	return parseAxQueryWasm(args)
}

func parseAxQueryWasm(args []string) (*types.AxFilter, error) {
	engine, err := wasm.GetEngine()
	if err != nil {
		return nil, errors.Wrap(err, "wasm engine init")
	}

	input := strings.Join(args, " ")

	resultJSON, err := engine.Call("parse_ax_query", input)
	if err != nil {
		return nil, errors.Wrap(err, "wasm parse_ax_query")
	}

	var rq rustAxQuery
	if err := json.Unmarshal([]byte(resultJSON), &rq); err != nil {
		return nil, errors.Wrap(err, "unmarshal wasm result")
	}

	if rq.Error != "" {
		return nil, errors.Newf("ax parse: %s", rq.Error)
	}

	return convertRustQuery(&rq)
}
