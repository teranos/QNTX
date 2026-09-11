package services

import (
	"testing"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
)

// A plugin's actor is its name. Source already holds it, so a plugin that names
// no actor is not an attestation nobody wrote — it is one the plugin wrote and
// did not say so.
func TestPluginActorFallsBackToSource(t *testing.T) {
	s := &ATSStoreServer{}

	cmd, err := s.protoToCommand(&protocol.AttestationCommand{
		Subjects:   []string{"inbox"},
		Predicates: []string{"weave"},
		Source:     "duif",
	})
	if err != nil {
		t.Fatalf("protoToCommand: %v", err)
	}

	if len(cmd.Actors) != 1 || cmd.Actors[0] != "duif" {
		t.Errorf("a plugin naming no actor should be its own actor, got %v", cmd.Actors)
	}
}

// Two actors can make contradictory claims and both are valid, so what a plugin
// names stands. The fallback only fills silence.
func TestPluginActorIsKeptWhenNamed(t *testing.T) {
	s := &ATSStoreServer{}

	cmd, err := s.protoToCommand(&protocol.AttestationCommand{
		Subjects:   []string{"inbox"},
		Predicates: []string{"weave"},
		Actors:     []string{"did:key:z6Mkalice"},
		Source:     "duif",
	})
	if err != nil {
		t.Fatalf("protoToCommand: %v", err)
	}

	if len(cmd.Actors) != 1 || cmd.Actors[0] != "did:key:z6Mkalice" {
		t.Errorf("a named actor should stand, got %v", cmd.Actors)
	}
}

// A plugin that names neither actor nor source is still attributed: source has
// its own fallback, and the actor follows it rather than being left empty.
func TestPluginActorFollowsTheSourceFallback(t *testing.T) {
	s := &ATSStoreServer{}

	cmd, err := s.protoToCommand(&protocol.AttestationCommand{
		Subjects:   []string{"inbox"},
		Predicates: []string{"weave"},
	})
	if err != nil {
		t.Fatalf("protoToCommand: %v", err)
	}

	if cmd.Source != "plugin" {
		t.Fatalf("source should fall back to 'plugin', got %q", cmd.Source)
	}
	if len(cmd.Actors) != 1 || cmd.Actors[0] != "plugin" {
		t.Errorf("the actor should follow the source it fell back to, got %v", cmd.Actors)
	}
}
