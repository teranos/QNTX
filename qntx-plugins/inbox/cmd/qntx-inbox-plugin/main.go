// qntx-inbox-plugin is a User's own mail (ADR-047).
package main

import (
	plugingrpc "github.com/teranos/QNTX/plugin/grpc"
	qntxinbox "github.com/teranos/QNTX/qntx-plugins/inbox"
)

func main() {
	plugingrpc.Run(qntxinbox.NewPlugin(), 9020)
}
