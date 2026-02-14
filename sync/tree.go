package sync

// SyncTree abstracts Merkle tree operations for attestation sync.
//
// The single implementation lives in Rust (qntx-core). On the server it's
// reached via wazero; in the browser via wasm-bindgen. This interface lets
// peer.go and observer.go stay implementation-agnostic.
//
// All hashes are hex-encoded strings — that's the natural representation from
// the WASM JSON API. No [32]byte ↔ hex conversions in Go.
type SyncTree interface {
	// Root returns the Merkle root hash as a 64-char hex string.
	// Empty tree returns a zero hash (64 zeros).
	Root() (string, error)

	// GroupHashes returns all (group key hash → group hash) pairs as hex.
	// Used during reconciliation: peers exchange these to find divergent groups
	// without transferring full attestation lists.
	GroupHashes() (map[string]string, error)

	// Diff compares local groups against remote group hashes.
	// Returns hex group key hashes for groups that are:
	//   - localOnly: exist locally but not remotely
	//   - remoteOnly: exist remotely but not locally
	//   - divergent: exist in both but have different group hashes
	Diff(remoteGroups map[string]string) (localOnly, remoteOnly, divergent []string, err error)

	// Contains checks if a content hash exists anywhere in the tree.
	Contains(contentHashHex string) (bool, error)

	// FindGroupKey reverse-looks up a group key hash → (actor, context).
	// During sync, peers exchange opaque group key hashes. When a peer receives
	// a Need message ("send me attestations for group abc123..."), it needs to
	// map that hash back to (actor, context) to query its attestation store.
	FindGroupKey(gkhHex string) (actor, context string, err error)

	// Insert adds a content hash under the (actor, context) group.
	// Called by the observer when a new attestation is created.
	Insert(actor, context, contentHashHex string) error

	// ContentHash computes the SHA-256 content hash of a JSON attestation.
	// The JSON must have timestamp as i64 milliseconds (Rust's format).
	// Returns a 64-char hex string.
	ContentHash(attestationJSON string) (string, error)
}
