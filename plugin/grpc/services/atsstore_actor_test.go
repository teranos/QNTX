package services

import (
	"testing"

	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"go.uber.org/zap"
)

// "the node"
//
// authors what a plugin asks it to: the plugin wrote through it.
func TestTheNodeAuthorsWhatAPluginAsksItTo(t *testing.T) {
	s := NewATSStoreServer(nil, "", "did:key:znode", zap.NewNop().Sugar())

	cmd, err := s.protoToCommand(&protocol.AttestationCommand{
		Subjects:   []string{"batch"},
		Predicates: []string{"crawl-timeout"},
		Source:     "collector",
		By:         &protocol.AttestationCommand_ByNode{ByNode: true},
		When:       &protocol.AttestationCommand_Now{Now: true},
		Version:    &protocol.AttestationCommand_SourceVersion{SourceVersion: "0.1.0"},
	})
	if err != nil {
		t.Fatalf("protoToCommand: %v", err)
	}

	actors, err := cmd.NamedActors()
	if err != nil {
		t.Fatalf("a command the node writes names no actor: %v", err)
	}
	if len(actors) != 1 || actors[0] != "did:key:znode" {
		t.Errorf("a plugin asking the node to write should be written by the node, got %v", actors)
	}
}

// "Name every meaning": a command naming no actor, no time or no version is
// refused, not written by the node, now, or as the registry has it.
func TestACommandNamingNeitherSideIsRefused(t *testing.T) {
	s := NewATSStoreServer(nil, "", "did:key:znode", zap.NewNop().Sugar())
	whole := func() *protocol.AttestationCommand {
		return &protocol.AttestationCommand{
			Subjects:   []string{"batch"},
			Predicates: []string{"crawl-timeout"},
			Source:     "collector",
			By:         &protocol.AttestationCommand_ActorsNamed{ActorsNamed: &protocol.ActorNames{Names: []string{"did:key:z6Mkalice"}}},
			When:       &protocol.AttestationCommand_Now{Now: true},
			Version:    &protocol.AttestationCommand_SourceVersion{SourceVersion: "0.1.0"},
		}
	}
	for name, unsaid := range map[string]func(*protocol.AttestationCommand){
		"no actor": func(c *protocol.AttestationCommand) { c.By = nil },
		"no actor named": func(c *protocol.AttestationCommand) {
			c.By = &protocol.AttestationCommand_ActorsNamed{ActorsNamed: &protocol.ActorNames{}}
		},
		"by_node false": func(c *protocol.AttestationCommand) { c.By = &protocol.AttestationCommand_ByNode{} },
		"no time":       func(c *protocol.AttestationCommand) { c.When = nil },
		"now false":     func(c *protocol.AttestationCommand) { c.When = &protocol.AttestationCommand_Now{} },
		"no version":    func(c *protocol.AttestationCommand) { c.Version = nil },
		"no source":     func(c *protocol.AttestationCommand) { c.Source = "" },
		"unknown source": func(c *protocol.AttestationCommand) {
			c.Version = &protocol.AttestationCommand_VersionFromRegistry{VersionFromRegistry: true}
		},
	} {
		cmd := whole()
		unsaid(cmd)
		if _, err := s.protoToCommand(cmd); err == nil {
			t.Errorf("%s: a command naming it was written", name)
		}
	}
	if _, err := s.protoToCommand(whole()); err != nil {
		t.Fatalf("a whole command was refused: %v", err)
	}
}

// Two actors can make contradictory claims and both are valid, so what a plugin
// names stands.
func TestPluginActorIsKeptWhenNamed(t *testing.T) {
	s := NewATSStoreServer(nil, "", "did:key:znode", zap.NewNop().Sugar())

	cmd, err := s.protoToCommand(&protocol.AttestationCommand{
		Subjects:   []string{"batch"},
		Predicates: []string{"crawl-timeout"},
		By:         &protocol.AttestationCommand_ActorsNamed{ActorsNamed: &protocol.ActorNames{Names: []string{"did:key:z6Mkalice"}}},
		Source:     "collector",
		When:       &protocol.AttestationCommand_Timestamp{Timestamp: 1700000000000},
		Version:    &protocol.AttestationCommand_SourceVersion{SourceVersion: "0.1.0"},
	})
	if err != nil {
		t.Fatalf("protoToCommand: %v", err)
	}

	actors, err := cmd.NamedActors()
	if err != nil {
		t.Fatalf("a command naming its actor names none: %v", err)
	}
	if len(actors) != 1 || actors[0] != "did:key:z6Mkalice" {
		t.Errorf("a named actor should stand, got %v", actors)
	}
}
