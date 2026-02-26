// qntx-atproto-plugin is an external gRPC plugin for the AT Protocol domain.
//
// Usage:
//
//	qntx-atproto-plugin --port 9001
//	qntx-atproto-plugin --address localhost:9001
package main

import (
	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
	qntxatproto "github.com/teranos/QNTX/qntx-atproto"
)

func main() {
	plugingrpc.Run(qntxatproto.NewPlugin(), 9001)
}
