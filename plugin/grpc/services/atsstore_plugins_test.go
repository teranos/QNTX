package services

import (
	"testing"

	"github.com/teranos/QNTX/ats"
	appcfg "github.com/teranos/QNTX/internal/config"
	"go.uber.org/zap"
)

// standingIn is a lookup that knows one plugin's token and the store it reaches.
func standingIn(token string, store ats.AttestationStore) PluginStores {
	return func(asked string) (ats.AttestationStore, bool) {
		if asked == token {
			return store, true
		}
		return nil, false
	}
}

// A plugin standing in a namespace of its own reads and writes there through
// the token the node handed it at Initialize (ADR-046); the shared token still
// reaches the served store, and a stranger's reaches nothing.
func TestAPluginsOwnTokenReachesTheNamespaceItStandsIn(t *testing.T) {
	served := &namedStore{name: "default"}
	clean := &namedStore{name: "clean"}
	s := NewATSStoreServer(served, "shared", zap.NewNop().Sugar())
	s.SetPluginStores(standingIn("cleanAPI", clean))

	got, err := s.storeFor("cleanAPI")
	if err != nil || got != clean {
		t.Fatalf("the plugin's token should reach the namespace it stands in, got %v, %v", got, err)
	}
	got, err = s.storeFor("shared")
	if err != nil || got != served {
		t.Fatalf("the shared token should reach the served store, got %v, %v", got, err)
	}
	if got, err := s.storeFor("stranger"); err == nil {
		t.Fatalf("a token nobody handed out reached %v", got)
	}
}

// A fetch a standing plugin asks for is attested into its namespace, not the
// served store.
func TestAFetchIsAttestedWhereThePluginStands(t *testing.T) {
	served := &namedStore{name: "default"}
	clean := &namedStore{name: "clean"}
	f := NewFetchServer(served, "shared", appcfg.FetchConfig{}, zap.NewNop().Sugar())
	t.Cleanup(f.Stop)
	f.SetPluginStores(standingIn("cleanAPI", clean))

	got, err := f.storeFor("cleanAPI")
	if err != nil || got != clean {
		t.Fatalf("the plugin's token should reach the namespace it stands in, got %v, %v", got, err)
	}
	got, err = f.storeFor("shared")
	if err != nil || got != served {
		t.Fatalf("the shared token should reach the served store, got %v, %v", got, err)
	}
	if got, err := f.storeFor("stranger"); err == nil {
		t.Fatalf("a token nobody handed out reached %v", got)
	}
}
