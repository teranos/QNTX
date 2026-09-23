package services

import (
	"testing"

	"github.com/teranos/QNTX/ats"
	"go.uber.org/zap"
)

// namedStore is a store told apart from another by name alone.
type namedStore struct {
	ats.AttestationStore
	name string
}

// A plugin answering a sigil reads and writes where the caller acts, through
// the token the node handed it for that call. The shared token is the served
// store, as it always was.
func TestAStoreIsTheOneItsTokenNames(t *testing.T) {
	served := &namedStore{name: "default"}
	caller := &namedStore{name: "vakconnectie"}
	s := NewATSStoreServer(served, "shared", zap.NewNop().Sugar())
	s.SetCallStores(func(token string) (ats.AttestationStore, bool) {
		if token == "call" {
			return caller, true
		}
		return nil, false
	})

	got, err := s.storeFor("shared")
	if err != nil || got != served {
		t.Fatalf("the shared token should reach the served store, got %v, %v", got, err)
	}
	got, err = s.storeFor("call")
	if err != nil || got != caller {
		t.Fatalf("a call's token should reach its caller's store, got %v, %v", got, err)
	}
	if got, err := s.storeFor("stranger"); err == nil {
		t.Fatalf("a token nobody handed out reached %v", got)
	}
}

// Before the node says which calls are open, no call token reaches anything.
func TestNoCallTokenReachesAStoreBeforeCallsAreKnown(t *testing.T) {
	s := NewATSStoreServer(&namedStore{name: "default"}, "shared", zap.NewNop().Sugar())

	if got, err := s.storeFor("call"); err == nil {
		t.Fatalf("a call token reached %v with no calls known", got)
	}
}
