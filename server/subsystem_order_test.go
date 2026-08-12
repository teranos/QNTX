package server

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func indexOfSubsystem(t *testing.T, name string) int {
	t.Helper()
	for i, entry := range subsystems {
		if entry.sub.Name() == name {
			return i
		}
	}
	t.Fatalf("no subsystem named %q in the init order", name)
	return -1
}

// The node DID names the system namespace, and namespace is the top-level
// prefix every other store writes under. Anything holding a store has to come
// after the identity that decides where that store lives.
func TestNodeDIDInitializesBeforeAnythingHoldingAStore(t *testing.T) {
	nodeDID := indexOfSubsystem(t, "node-did")

	require.Less(t, nodeDID, indexOfSubsystem(t, "auth"),
		"auth opens the token store, which is namespace-scoped (ADR-027)")
	require.Less(t, nodeDID, indexOfSubsystem(t, "watcher"),
		"watchers are namespace-scoped (ADR-026)")
	require.Less(t, nodeDID, indexOfSubsystem(t, "canvas"),
		"a canvas lives in one namespace (ADR-026)")
}
