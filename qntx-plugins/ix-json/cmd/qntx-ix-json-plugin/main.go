// qntx-ix-json-plugin is an external gRPC plugin for JSON API ingestion.
//
// Usage:
//
//	qntx-ix-json-plugin --port 9002
//	qntx-ix-json-plugin --address localhost:9002
package main

import (
	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
	qntxixjson "github.com/teranos/QNTX/qntx-plugins/ix-json"
)

func main() {
	plugingrpc.Run(qntxixjson.NewPlugin(), 9002)
}
