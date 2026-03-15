//go:build kern

package parser

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/errors"
	"github.com/teranos/QNTX/plugin"
	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
)

// parseAxQueryDispatch calls the kern plugin's ParseAxQuery RPC via gRPC.
func parseAxQueryDispatch(args []string, verbosity int, ctx ErrorContext) (*types.AxFilter, error) {
	input := strings.Join(args, " ")

	registry := plugin.GetDefaultRegistry()
	if registry == nil {
		return nil, errors.New("plugin registry not initialized")
	}

	dp, ok := registry.Get("kern")
	if !ok {
		return nil, errors.New("kern plugin not registered — is it enabled in config?")
	}

	proxy, ok := dp.(*plugingrpc.ExternalDomainProxy)
	if !ok {
		return nil, errors.New("kern plugin is not a gRPC plugin")
	}

	rpcCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resultJSON, err := proxy.ParseAxQuery(rpcCtx, input)
	if err != nil {
		return nil, errors.Wrap(err, "kern parse")
	}

	var rq rustAxQuery
	if err := json.Unmarshal(resultJSON, &rq); err != nil {
		return nil, errors.Wrapf(err, "kern: unmarshal result: %s", string(resultJSON))
	}

	if rq.Error != "" {
		return nil, errors.Newf("ax parse: %s", rq.Error)
	}

	return convertRustQuery(&rq)
}
