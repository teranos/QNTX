package sync

import (
	"testing"
	"time"

	"github.com/teranos/QNTX/ats/types"
)

func TestTreeObserver_InsertsIntoTree(t *testing.T) {
	tree := NewTree()
	obs := NewTreeObserver(tree)

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

	if tree.Size() != 1 {
		t.Fatalf("expected tree size 1, got %d", tree.Size())
	}

	if tree.GroupCount() != 1 {
		t.Fatalf("expected 1 group, got %d", tree.GroupCount())
	}

	leaves := tree.GroupLeaves(GroupKey{Actor: "hr-system", Context: "team-eng"})
	if len(leaves) != 1 {
		t.Fatalf("expected 1 leaf in group, got %d", len(leaves))
	}

	expected := ContentHash(as)
	if leaves[0] != expected {
		t.Fatal("leaf hash doesn't match content hash")
	}
}

func TestTreeObserver_MultipleActorsContexts(t *testing.T) {
	tree := NewTree()
	obs := NewTreeObserver(tree)

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
	if tree.GroupCount() != 4 {
		t.Fatalf("expected 4 groups (2 actors × 2 contexts), got %d", tree.GroupCount())
	}

	// Each group should have 1 leaf (the same content hash)
	ch := ContentHash(as)
	for _, actor := range as.Actors {
		for _, ctx := range as.Contexts {
			leaves := tree.GroupLeaves(GroupKey{Actor: actor, Context: ctx})
			if len(leaves) != 1 {
				t.Fatalf("group (%s, %s): expected 1 leaf, got %d", actor, ctx, len(leaves))
			}
			if leaves[0] != ch {
				t.Fatalf("group (%s, %s): leaf hash mismatch", actor, ctx)
			}
		}
	}
}

func TestTreeObserver_NilAttestation(t *testing.T) {
	tree := NewTree()
	obs := NewTreeObserver(tree)

	// Should not panic
	obs.OnAttestationCreated(nil)

	if tree.Size() != 0 {
		t.Fatal("nil attestation should not add to tree")
	}
}

func TestTreeObserver_RootChangesWithAttestations(t *testing.T) {
	tree := NewTree()
	obs := NewTreeObserver(tree)

	empty := tree.Root()

	obs.OnAttestationCreated(&types.As{
		Subjects:   []string{"s1"},
		Predicates: []string{"p1"},
		Contexts:   []string{"c1"},
		Actors:     []string{"a1"},
		Timestamp:  time.Now(),
		Source:     "cli",
	})

	after := tree.Root()
	if after == empty {
		t.Fatal("root should change after attestation creation")
	}
}

func TestTreeObserver_TreeAccessor(t *testing.T) {
	tree := NewTree()
	obs := NewTreeObserver(tree)

	if obs.Tree() != tree {
		t.Fatal("Tree() should return the underlying tree")
	}
}
