// qntx-atproto-plugin is an external gRPC plugin for the AT Protocol domain.
//
// Usage:
//
//	qntx-atproto-plugin --port 9001
//	qntx-atproto-plugin --address localhost:9001
package main

import (
	"github.com/teranos/QNTX/plugin/grpc/pluginmain"
	qntxatproto "github.com/teranos/QNTX/qntx-atproto"
)

func main() {
	pluginmain.Run(qntxatproto.NewPlugin(), 9001)
}
