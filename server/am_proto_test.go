package server

import (
	"context"
	"slices"
	"testing"

	"github.com/teranos/QNTX/internal/version"
)

// am version and am node answer Go structs for their json tags, and proto
// declares their shape (ADR-006): the two are held to the same fields here.

func TestAmVersionIsTheShapeProtoDeclares(t *testing.T) {
	declared := fieldsOf(t, watcherProto, "VersionInfo")
	tagged := jsonNamesOf(t, version.Info{})
	if !slices.Equal(declared, tagged) {
		t.Errorf("VersionInfo declares %v, and version.Info is tagged %v", declared, tagged)
	}
}

func TestAmNodeIsTheShapeProtoDeclares(t *testing.T) {
	declared := fieldsOf(t, "../plugin/grpc/protocol/sigil.proto", "Node")
	tagged := jsonNamesOf(t, amNode{})
	if !slices.Equal(declared, tagged) {
		t.Errorf("Node declares %v, and amNode is tagged %v", declared, tagged)
	}
}

// am node says no name it was not given, and lists the signa it serves.
func TestAmNodeGivesWhatItHas(t *testing.T) {
	s := &QNTXServer{}
	signum := s.amSignum()
	answer, refused := signum.Answers["node"](context.Background(), nil)
	if refused != nil {
		t.Fatalf("node refused: %s", refused.GetSays())
	}
	node := answer.(amNode)
	if node.Name != "" || node.Description != "" {
		t.Errorf("a node with no name was given %q, %q", node.Name, node.Description)
	}
	var names []string
	for _, held := range node.Signa {
		names = append(names, held.GetName())
	}
	if !slices.Contains(names, "am") || !slices.Contains(names, "parity") {
		t.Errorf("node lists %v", names)
	}
	holds(t, signum, "node", answer)
}
