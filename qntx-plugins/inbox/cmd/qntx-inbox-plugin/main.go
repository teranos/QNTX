// qntx-inbox-plugin is a User's own mail (ADR-047).
package main

import (
	qntxinbox "github.com/teranos/QNTX/qntx-plugins/inbox"
	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
)

func main() {
	plugingrpc.Run(qntxinbox.NewPlugin(), 9020)
}
