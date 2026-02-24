// qntx-github-plugin is an external gRPC plugin for the GitHub domain.
//
// Usage:
//
//	qntx-github-plugin --port 9002
//	qntx-github-plugin --address localhost:9002
package main

import (
	"github.com/teranos/QNTX/plugin/grpc/pluginmain"
	qntxgithub "github.com/teranos/qntx-github"
)

func main() {
	pluginmain.Run(qntxgithub.NewPlugin(), 9002)
}
