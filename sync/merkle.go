package sync

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	gosync "sync"
)

// Hash is a SHA-256 content hash used as a node identifier in the Merkle tree.
type Hash = [32]byte

// Tree is an in-memory Merkle tree that mirrors the bounded storage hierarchy.
//
// Structure:
//
//	Root
//	└── Group (actor, context pair)
//	    └── Leaf (attestation content hash)
//
// The tree supports O(1) insert/remove and O(groups) root recomputation.
// Groups map directly to bounded storage's (actor, context) pairs — the unit
// at which eviction and sync reconciliation operate.
type Tree struct {
	mu     gosync.RWMutex
	groups map[Hash]*group // keyed by GroupKey hash
	dirty  bool            // root needs recomputation
	root   Hash
}

// group is one (actor, context) bucket in the tree, containing up to 16
// attestation content hashes (matching the bounded storage default).
type group struct {
	key    GroupKey
	leaves map[Hash]struct{}
	dirty  bool
	hash   Hash
}

// GroupKey identifies a bounded storage group: one (actor, context) pair.
type GroupKey struct {
	Actor   string
	Context string
}

// groupKeyHash returns a deterministic hash of a GroupKey.
func groupKeyHash(k GroupKey) Hash {
	h := sha256.New()
	h.Write([]byte("gk:"))
	h.Write([]byte(k.Actor))
	h.Write([]byte("\x00"))
	h.Write([]byte(k.Context))
	var out Hash
	h.Sum(out[:0])
	return out
}

// NewTree creates an empty Merkle tree.
func NewTree() *Tree {
	return &Tree{
		groups: make(map[Hash]*group),
	}
}

// Insert adds an attestation content hash under the given group.
// The tree root is lazily recomputed on the next call to Root().
func (t *Tree) Insert(key GroupKey, contentHash Hash) {
	t.mu.Lock()
	defer t.mu.Unlock()

	gkh := groupKeyHash(key)
	g, ok := t.groups[gkh]
	if !ok {
		g = &group{
			key:    key,
			leaves: make(map[Hash]struct{}),
		}
		t.groups[gkh] = g
	}

	if _, exists := g.leaves[contentHash]; exists {
		return // already present
	}

	g.leaves[contentHash] = struct{}{}
	g.dirty = true
	t.dirty = true
}

// Remove deletes an attestation content hash from the given group.
// If the group becomes empty, it is removed from the tree.
func (t *Tree) Remove(key GroupKey, contentHash Hash) {
	t.mu.Lock()
	defer t.mu.Unlock()

	gkh := groupKeyHash(key)
	g, ok := t.groups[gkh]
	if !ok {
		return
	}

	if _, exists := g.leaves[contentHash]; !exists {
		return
	}

	delete(g.leaves, contentHash)
	g.dirty = true
	t.dirty = true

	if len(g.leaves) == 0 {
		delete(t.groups, gkh)
	}
}

// Root returns the current Merkle root hash. Recomputes lazily when dirty.
// An empty tree has a zero hash.
func (t *Tree) Root() Hash {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.dirty {
		return t.root
	}

	t.recompute()
	return t.root
}

// GroupHashes returns a map of group key hash → group hash for all groups.
// Used during sync reconciliation: peers exchange group hashes to find
// divergent groups without transferring full attestation lists.
func (t *Tree) GroupHashes() map[Hash]Hash {
	t.mu.Lock()
	defer t.mu.Unlock()

	result := make(map[Hash]Hash, len(t.groups))
	for gkh, g := range t.groups {
		if g.dirty {
			g.recomputeHash()
		}
		result[gkh] = g.hash
	}
	return result
}

// GroupLeaves returns all attestation content hashes in a group.
// Returns nil if the group doesn't exist.
func (t *Tree) GroupLeaves(key GroupKey) []Hash {
	t.mu.RLock()
	defer t.mu.RUnlock()

	gkh := groupKeyHash(key)
	g, ok := t.groups[gkh]
	if !ok {
		return nil
	}

	leaves := make([]Hash, 0, len(g.leaves))
	for h := range g.leaves {
		leaves = append(leaves, h)
	}
	return leaves
}

// Size returns the total number of attestation content hashes in the tree.
func (t *Tree) Size() int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	n := 0
	for _, g := range t.groups {
		n += len(g.leaves)
	}
	return n
}

// GroupCount returns the number of (actor, context) groups in the tree.
func (t *Tree) GroupCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.groups)
}

// Diff compares this tree's group hashes against a remote peer's group hashes
// and returns three sets:
//   - localOnly: group key hashes that exist locally but not remotely
//   - remoteOnly: group key hashes that exist remotely but not locally
//   - divergent: group key hashes that exist in both but have different group hashes
func (t *Tree) Diff(remoteGroups map[Hash]Hash) (localOnly, remoteOnly []Hash, divergent []Hash) {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Ensure all group hashes are current
	for _, g := range t.groups {
		if g.dirty {
			g.recomputeHash()
		}
	}

	// Find local-only and divergent
	for gkh, g := range t.groups {
		remoteHash, exists := remoteGroups[gkh]
		if !exists {
			localOnly = append(localOnly, gkh)
		} else if remoteHash != g.hash {
			divergent = append(divergent, gkh)
		}
	}

	// Find remote-only
	for gkh := range remoteGroups {
		if _, exists := t.groups[gkh]; !exists {
			remoteOnly = append(remoteOnly, gkh)
		}
	}

	return
}

// recompute recalculates the root hash from group hashes.
// Caller must hold t.mu.
func (t *Tree) recompute() {
	if len(t.groups) == 0 {
		t.root = Hash{}
		t.dirty = false
		return
	}

	// Collect all group hashes, sort them for determinism
	hashes := make([]Hash, 0, len(t.groups))
	for _, g := range t.groups {
		if g.dirty {
			g.recomputeHash()
		}
		hashes = append(hashes, g.hash)
	}

	sortHashes(hashes)

	h := sha256.New()
	h.Write([]byte("root:"))
	for _, gh := range hashes {
		h.Write(gh[:])
	}
	h.Sum(t.root[:0])
	t.dirty = false
}

// recomputeHash recalculates the group hash from its leaf hashes.
func (g *group) recomputeHash() {
	if len(g.leaves) == 0 {
		g.hash = Hash{}
		g.dirty = false
		return
	}

	hashes := make([]Hash, 0, len(g.leaves))
	for h := range g.leaves {
		hashes = append(hashes, h)
	}
	sortHashes(hashes)

	hasher := sha256.New()
	hasher.Write([]byte("grp:"))
	// Include the group key in the hash so identical leaf sets under
	// different (actor, context) pairs produce different group hashes.
	hasher.Write([]byte(g.key.Actor))
	hasher.Write([]byte("\x00"))
	hasher.Write([]byte(g.key.Context))
	hasher.Write([]byte("\x00"))
	for _, h := range hashes {
		hasher.Write(h[:])
	}
	hasher.Sum(g.hash[:0])
	g.dirty = false
}

// sortHashes sorts a slice of hashes lexicographically.
func sortHashes(hashes []Hash) {
	sort.Slice(hashes, func(i, j int) bool {
		for k := 0; k < 32; k++ {
			if hashes[i][k] != hashes[j][k] {
				return hashes[i][k] < hashes[j][k]
			}
		}
		return false
	})
}

// HexHash returns the hex-encoded string of a Hash (for logging/debugging).
func HexHash(h Hash) string {
	return hex.EncodeToString(h[:])
}
