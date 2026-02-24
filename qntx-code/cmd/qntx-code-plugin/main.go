// qntx-code-plugin is an external gRPC plugin for the code domain.
//
// Usage:
//
//	qntx-code-plugin --port 9000
//	qntx-code-plugin --address localhost:9000
package main

import (
	"github.com/teranos/QNTX/plugin/grpc/pluginmain"
	qntxcode "github.com/teranos/QNTX/qntx-code"
)

func main() {
	pluginmain.Run(qntxcode.NewPlugin(), 9000)
}
