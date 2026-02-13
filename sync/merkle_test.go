package sync

import (
	"crypto/sha256"
	"testing"
)

func hash(s string) Hash {
	return sha256.Sum256([]byte(s))
}

func TestTree_EmptyRoot(t *testing.T) {
	tree := NewTree()
	root := tree.Root()
	if root != (Hash{}) {
		t.Fatalf("empty tree should have zero root, got %x", root)
	}
}

func TestTree_InsertChangesRoot(t *testing.T) {
	tree := NewTree()
	empty := tree.Root()

	tree.Insert(GroupKey{"actor", "ctx"}, hash("attestation-1"))

	if tree.Root() == empty {
		t.Fatal("root should change after insert")
	}
}

func TestTree_DeterministicRoot(t *testing.T) {
	// Two trees with the same data inserted in different order should
	// produce the same root hash.
	a := NewTree()
	b := NewTree()

	k1 := GroupKey{"actor-1", "ctx-1"}
	k2 := GroupKey{"actor-2", "ctx-2"}
	h1 := hash("att-1")
	h2 := hash("att-2")
	h3 := hash("att-3")

	a.Insert(k1, h1)
	a.Insert(k1, h2)
	a.Insert(k2, h3)

	b.Insert(k2, h3)
	b.Insert(k1, h2)
	b.Insert(k1, h1)

	if a.Root() != b.Root() {
		t.Fatalf("trees with same data should have same root: %x vs %x", a.Root(), b.Root())
	}
}

func TestTree_RemoveRestoresRoot(t *testing.T) {
	tree := NewTree()
	empty := tree.Root()

	key := GroupKey{"actor", "ctx"}
	ch := hash("att-1")

	tree.Insert(key, ch)
	tree.Remove(key, ch)

	if tree.Root() != empty {
		t.Fatalf("root should return to zero after removing all entries, got %x", tree.Root())
	}
}

func TestTree_RemoveNonexistent(t *testing.T) {
	tree := NewTree()
	key := GroupKey{"actor", "ctx"}

	// Should not panic
	tree.Remove(key, hash("does-not-exist"))
	tree.Remove(GroupKey{"no", "group"}, hash("anything"))
}

func TestTree_DuplicateInsert(t *testing.T) {
	tree := NewTree()
	key := GroupKey{"actor", "ctx"}
	ch := hash("att-1")

	tree.Insert(key, ch)
	root1 := tree.Root()

	tree.Insert(key, ch) // duplicate
	root2 := tree.Root()

	if root1 != root2 {
		t.Fatal("duplicate insert should not change root")
	}

	if tree.Size() != 1 {
		t.Fatalf("expected size 1 after duplicate insert, got %d", tree.Size())
	}
}

func TestTree_Size(t *testing.T) {
	tree := NewTree()
	if tree.Size() != 0 {
		t.Fatal("empty tree should have size 0")
	}

	k1 := GroupKey{"a", "c1"}
	k2 := GroupKey{"a", "c2"}

	tree.Insert(k1, hash("1"))
	tree.Insert(k1, hash("2"))
	tree.Insert(k2, hash("3"))

	if tree.Size() != 3 {
		t.Fatalf("expected size 3, got %d", tree.Size())
	}
}

func TestTree_GroupCount(t *testing.T) {
	tree := NewTree()

	tree.Insert(GroupKey{"a", "c1"}, hash("1"))
	tree.Insert(GroupKey{"a", "c1"}, hash("2"))
	tree.Insert(GroupKey{"a", "c2"}, hash("3"))
	tree.Insert(GroupKey{"b", "c1"}, hash("4"))

	if tree.GroupCount() != 3 {
		t.Fatalf("expected 3 groups, got %d", tree.GroupCount())
	}
}

func TestTree_GroupLeaves(t *testing.T) {
	tree := NewTree()
	key := GroupKey{"actor", "ctx"}
	h1 := hash("1")
	h2 := hash("2")

	tree.Insert(key, h1)
	tree.Insert(key, h2)

	leaves := tree.GroupLeaves(key)
	if len(leaves) != 2 {
		t.Fatalf("expected 2 leaves, got %d", len(leaves))
	}

	// Verify both hashes present
	found := map[Hash]bool{}
	for _, l := range leaves {
		found[l] = true
	}
	if !found[h1] || !found[h2] {
		t.Fatal("missing expected leaf hashes")
	}
}

func TestTree_GroupLeaves_NonexistentGroup(t *testing.T) {
	tree := NewTree()
	leaves := tree.GroupLeaves(GroupKey{"no", "group"})
	if leaves != nil {
		t.Fatalf("expected nil for nonexistent group, got %v", leaves)
	}
}

func TestTree_GroupHashes(t *testing.T) {
	tree := NewTree()
	k1 := GroupKey{"a", "c1"}
	k2 := GroupKey{"b", "c2"}

	tree.Insert(k1, hash("1"))
	tree.Insert(k2, hash("2"))

	gh := tree.GroupHashes()
	if len(gh) != 2 {
		t.Fatalf("expected 2 group hashes, got %d", len(gh))
	}

	gk1 := groupKeyHash(k1)
	gk2 := groupKeyHash(k2)

	if _, ok := gh[gk1]; !ok {
		t.Fatal("missing group hash for k1")
	}
	if _, ok := gh[gk2]; !ok {
		t.Fatal("missing group hash for k2")
	}
}

func TestTree_Diff_Identical(t *testing.T) {
	a := NewTree()
	b := NewTree()

	key := GroupKey{"actor", "ctx"}
	ch := hash("att-1")

	a.Insert(key, ch)
	b.Insert(key, ch)

	localOnly, remoteOnly, divergent := a.Diff(b.GroupHashes())

	if len(localOnly) != 0 || len(remoteOnly) != 0 || len(divergent) != 0 {
		t.Fatalf("identical trees should have no diff: local=%d remote=%d divergent=%d",
			len(localOnly), len(remoteOnly), len(divergent))
	}
}

func TestTree_Diff_LocalOnly(t *testing.T) {
	a := NewTree()
	b := NewTree()

	k1 := GroupKey{"actor", "ctx-1"}
	k2 := GroupKey{"actor", "ctx-2"}

	a.Insert(k1, hash("1"))
	a.Insert(k2, hash("2"))
	b.Insert(k1, hash("1"))

	localOnly, remoteOnly, divergent := a.Diff(b.GroupHashes())

	if len(localOnly) != 1 {
		t.Fatalf("expected 1 local-only group, got %d", len(localOnly))
	}
	if len(remoteOnly) != 0 || len(divergent) != 0 {
		t.Fatalf("expected no remote-only or divergent, got remote=%d divergent=%d",
			len(remoteOnly), len(divergent))
	}
}

func TestTree_Diff_Divergent(t *testing.T) {
	a := NewTree()
	b := NewTree()

	key := GroupKey{"actor", "ctx"}

	a.Insert(key, hash("att-1"))
	b.Insert(key, hash("att-2"))

	localOnly, remoteOnly, divergent := a.Diff(b.GroupHashes())

	if len(divergent) != 1 {
		t.Fatalf("expected 1 divergent group, got %d", len(divergent))
	}
	if len(localOnly) != 0 || len(remoteOnly) != 0 {
		t.Fatalf("expected no local-only or remote-only, got local=%d remote=%d",
			len(localOnly), len(remoteOnly))
	}
}

func TestTree_Diff_RemoteOnly(t *testing.T) {
	a := NewTree()
	b := NewTree()

	k1 := GroupKey{"actor", "ctx-1"}
	k2 := GroupKey{"actor", "ctx-2"}

	a.Insert(k1, hash("1"))
	b.Insert(k1, hash("1"))
	b.Insert(k2, hash("2"))

	localOnly, remoteOnly, divergent := a.Diff(b.GroupHashes())

	if len(remoteOnly) != 1 {
		t.Fatalf("expected 1 remote-only group, got %d", len(remoteOnly))
	}
	if len(localOnly) != 0 || len(divergent) != 0 {
		t.Fatalf("expected no local-only or divergent, got local=%d divergent=%d",
			len(localOnly), len(divergent))
	}
}

func TestTree_DifferentGroupsSameLeaves(t *testing.T) {
	// Two groups with the same leaf content must produce different group hashes
	// because the group key is mixed into the hash.
	a := NewTree()
	k1 := GroupKey{"actor-1", "ctx"}
	k2 := GroupKey{"actor-2", "ctx"}
	ch := hash("same-content")

	a.Insert(k1, ch)
	a.Insert(k2, ch)

	gh := a.GroupHashes()
	gk1 := groupKeyHash(k1)
	gk2 := groupKeyHash(k2)

	if gh[gk1] == gh[gk2] {
		t.Fatal("different groups with same leaves should have different group hashes")
	}
}

func TestHexHash(t *testing.T) {
	h := hash("test")
	s := HexHash(h)
	if len(s) != 64 {
		t.Fatalf("expected 64 hex chars, got %d", len(s))
	}
}
