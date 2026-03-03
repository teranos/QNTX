// qntx-vidstream-plugin is an external gRPC plugin for real-time video inference.
//
// Wraps the Rust ONNX video engine via CGO, exposing frame processing as HTTP
// endpoints. Registers a canvas glyph with window manifestation.
//
// Build:
//
//	CGO_ENABLED=1 go build -tags rustvideo ./qntx-vidstream/cmd/qntx-vidstream-plugin
//
// Usage:
//
//	qntx-vidstream-plugin --port 9200
package main

import (
	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
	qntxvidstream "github.com/teranos/QNTX/qntx-vidstream"
)

func main() {
	plugingrpc.Run(qntxvidstream.NewPlugin(), 9200)
}
