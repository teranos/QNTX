// Package sync provides content-addressed attestation identity and Merkle tree
// state digests for peer-to-peer attestation synchronization.
//
// Content hashing produces a deterministic digest from an attestation's semantic
// fields (subjects, predicates, contexts, actors, timestamp, source). Two nodes
// creating the same claim independently will produce the same content hash,
// enabling deduplication and set reconciliation without sharing ASIDs.
//
// The Merkle tree mirrors the bounded storage hierarchy (entity → actor →
// context → attestation) and supports O(log n) state comparison between peers.
package sync

import (
	"crypto/sha256"
	"encoding/binary"
	"sort"
	"strings"

	"github.com/teranos/QNTX/ats/types"
)

// ContentHash computes a deterministic SHA-256 digest from an attestation's
// semantic fields. The hash covers subjects, predicates, contexts, actors,
// timestamp, and source — everything that defines the claim. Attributes and
// CreatedAt are excluded: attributes are mutable metadata, and CreatedAt is
// a local database artifact.
//
// Two attestations with identical semantic content produce the same hash
// regardless of ASID, attributes, or creation time.
func ContentHash(as *types.As) [32]byte {
	h := sha256.New()

	// Each field separated by a domain separator to prevent collisions
	// between fields (e.g., subject "a\x00b" vs subjects ["a","b"]).
	h.Write([]byte("s:"))
	h.Write(canonical(as.Subjects))
	h.Write([]byte("\np:"))
	h.Write(canonical(as.Predicates))
	h.Write([]byte("\nc:"))
	h.Write(canonical(as.Contexts))
	h.Write([]byte("\na:"))
	h.Write(canonical(as.Actors))
	h.Write([]byte("\nt:"))
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(as.Timestamp.UnixNano()))
	h.Write(ts[:])
	h.Write([]byte("\nrc:"))
	h.Write([]byte(as.Source))

	var out [32]byte
	h.Sum(out[:0])
	return out
}

// canonical sorts a string slice and joins elements with null bytes.
// The sort ensures determinism regardless of input order.
// A copy is made to avoid mutating the input.
func canonical(ss []string) []byte {
	sorted := make([]string, len(ss))
	copy(sorted, ss)
	sort.Strings(sorted)
	return []byte(strings.Join(sorted, "\x00"))
}
