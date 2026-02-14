package sync

import (
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

func TestTreeObserver_InsertsIntoTree(t *testing.T) {
	tree := newMemTree()
	obs := NewTreeObserver(tree, testLogger())

	as := &types.As{
		ID:         "as-test-1",
		Subjects:   []string{"user-1"},
		Predicates: []string{"member"},
		Contexts:   []string{"team-eng"},
		Actors:     []string{"hr-system"},
		Timestamp:  time.Now(),
		Source:     "cli",
	}

	obs.OnAttestationCreated(as)

	// Should have 1 group with 1 leaf
	groups, _ := tree.GroupHashes()
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	// Content hash should be findable
	aj, err := attestationJSON(as)
	if err != nil {
		t.Fatal(err)
	}
	chHex, _ := tree.ContentHash(aj)
	exists, _ := tree.Contains(chHex)
	if !exists {
		t.Fatal("tree should contain the attestation's content hash")
	}
}

func TestTreeObserver_MultipleActorsContexts(t *testing.T) {
	tree := newMemTree()
	obs := NewTreeObserver(tree, testLogger())

	as := &types.As{
		ID:         "as-test-2",
		Subjects:   []string{"doc-1"},
		Predicates: []string{"reviewed"},
		Contexts:   []string{"pr-123", "sprint-5"},
		Actors:     []string{"alice", "bob"},
		Timestamp:  time.Now(),
		Source:     "api",
	}

	obs.OnAttestationCreated(as)

	// 2 actors × 2 contexts = 4 groups
	groups, _ := tree.GroupHashes()
	if len(groups) != 4 {
		t.Fatalf("expected 4 groups (2 actors × 2 contexts), got %d", len(groups))
	}
}

func TestTreeObserver_NilAttestation(t *testing.T) {
	tree := newMemTree()
	obs := NewTreeObserver(tree, testLogger())

	// Should not panic
	obs.OnAttestationCreated(nil)

	groups, _ := tree.GroupHashes()
	if len(groups) != 0 {
		t.Fatal("nil attestation should not add to tree")
	}
}

func TestTreeObserver_RootChangesWithAttestations(t *testing.T) {
	tree := newMemTree()
	obs := NewTreeObserver(tree, testLogger())

	empty, _ := tree.Root()

	obs.OnAttestationCreated(&types.As{
		Subjects:   []string{"s1"},
		Predicates: []string{"p1"},
		Contexts:   []string{"c1"},
		Actors:     []string{"a1"},
		Timestamp:  time.Now(),
		Source:     "cli",
	})

	after, _ := tree.Root()
	if after == empty {
		t.Fatal("root should change after attestation creation")
	}
}

func TestTreeObserver_TreeAccessor(t *testing.T) {
	tree := newMemTree()
	obs := NewTreeObserver(tree, testLogger())

	if obs.Tree() != tree {
		t.Fatal("Tree() should return the underlying tree")
	}
}
