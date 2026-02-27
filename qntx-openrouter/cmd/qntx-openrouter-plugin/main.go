// qntx-openrouter-plugin is an external gRPC plugin for OpenRouter LLM integration.
//
// Usage:
//
//	qntx-openrouter-plugin --port 9100
//	qntx-openrouter-plugin --address localhost:9100
package main

import (
	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
	qntxopenrouter "github.com/teranos/qntx-openrouter"
)

func main() {
	plugingrpc.Run(qntxopenrouter.NewPlugin(), 9100)
}
