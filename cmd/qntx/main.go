// Command qntx is the node. Everything it does is in cmd/qntx/qntx, so that a
// binary carrying a subsystem of its own is this file with one more import
// (ADR-056): the subsystem's package, which registers itself as it loads.
package main

import "github.com/teranos/QNTX/cmd/qntx/qntx"

func main() {
	qntx.Main()
}
