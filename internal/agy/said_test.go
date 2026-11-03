package agy

import (
	"context"
	"testing"
)

// 「[Opus 5.5] Without its own key agy reaches for a Google sign-in, in a browser or the」
// 「[Opus 5.5] node's keyring: neither is the agent's.」
func TestRunRequiresExplicitConfiguration(t *testing.T) {
	for _, s := range []Said{{}, {Binary: "unused", Home: t.TempDir(), Google: &Google{}, Says: "x"}, {Binary: "unused", Home: t.TempDir(), Model: "test", Says: "x"}} {
		if _, err := s.Run(context.Background(), nil); err == nil {
			t.Fatal("accepted incomplete configuration")
		}
	}
}
