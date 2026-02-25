// qntx-code-plugin is an external gRPC plugin for the code domain.
//
// Usage:
//
//	qntx-code-plugin --port 9000
//	qntx-code-plugin --address localhost:9000
package main

import (
	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
	qntxcode "github.com/teranos/QNTX/qntx-code"
)

func main() {
	plugingrpc.Run(qntxcode.NewPlugin(), 9000)
}
