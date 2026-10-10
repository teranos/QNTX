package services

import (
	"testing"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
)

// "the node"
//
// authors what a plugin named no actor for: the plugin wrote through it.
func TestTheNodeAuthorsWhatAPluginNamedNoActorFor(t *testing.T) {
	s := NewATSStoreServer(nil, "", "did:key:znode", zap.NewNop().Sugar())

	cmd, err := s.protoToCommand(&protocol.AttestationCommand{
		Subjects:   []string{"batch"},
		Predicates: []string{"crawl-timeout"},
		Source:     "collector",
	})
	if err != nil {
		t.Fatalf("protoToCommand: %v", err)
	}

	if len(cmd.Actors) != 1 || cmd.Actors[0] != "did:key:znode" {
		t.Errorf("a plugin naming no actor should be written by the node, got %v", cmd.Actors)
	}
}

// Two actors can make contradictory claims and both are valid, so what a plugin
// names stands.
func TestPluginActorIsKeptWhenNamed(t *testing.T) {
	s := NewATSStoreServer(nil, "", "did:key:znode", zap.NewNop().Sugar())

	cmd, err := s.protoToCommand(&protocol.AttestationCommand{
		Subjects:   []string{"batch"},
		Predicates: []string{"crawl-timeout"},
		Actors:     []string{"did:key:z6Mkalice"},
		Source:     "collector",
	})
	if err != nil {
		t.Fatalf("protoToCommand: %v", err)
	}

	if len(cmd.Actors) != 1 || cmd.Actors[0] != "did:key:z6Mkalice" {
		t.Errorf("a named actor should stand, got %v", cmd.Actors)
	}
}
