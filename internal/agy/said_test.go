package agy

import (
	"context"
	"testing"
)

// Without its own key agy reaches for a Google sign-in, in a browser or the
// node's keyring: neither is the agent's.
func TestRunRequiresExplicitConfiguration(t *testing.T) {
	for _, s := range []Said{{}, {Binary: "unused", Home: t.TempDir(), APIKey: "k", Says: "x"}, {Binary: "unused", Home: t.TempDir(), Model: "test", Says: "x"}} {
		if _, err := s.Run(context.Background(), nil); err == nil {
			t.Fatal("accepted incomplete configuration")
		}
	}
}
