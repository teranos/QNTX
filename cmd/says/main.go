// Command says writes what every message and field of the protocol's .proto
// files says of itself to server/parity/protocol.says.json, which the node
// embeds: the generated Go does not keep a .proto's comments, and the parity
// sigil gives them beside what each spec says ("seeing prose from the specs
// themselves where they have it").
package main

import (
	"fmt"
	"os"

	"github.com/teranos/QNTX/server/parity"
	"github.com/teranos/errors"
)

func main() {
	dir := "plugin/grpc/protocol"
	says, err := parity.ProtocolSays(os.DirFS(dir))
	if err != nil {
		fmt.Fprintf(os.Stderr, "says: %v\n", errors.Wrapf(err, "failed to read %s", dir))
		os.Exit(1)
	}
	body, err := parity.WriteProtocolSays(says)
	if err != nil {
		fmt.Fprintf(os.Stderr, "says: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(parity.ProtocolSaysFile, body, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "says: %v\n", errors.Wrapf(err, "failed to write %s", parity.ProtocolSaysFile))
		os.Exit(1)
	}
	fmt.Printf("%d messages and fields of %s say something of themselves, written to %s\n", len(says), dir, parity.ProtocolSaysFile)
}
