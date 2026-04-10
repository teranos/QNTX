package reticulum

import (
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"

	"github.com/teranos/QNTX/errors"
	"golang.org/x/crypto/curve25519"
)

// Destination is a 16-byte Reticulum destination hash.
// Derived from SHA-256(SHA-256(name) + public_keys)[:16].
type Destination [16]byte

// String returns the destination hash as a hex string.
func (d Destination) String() string {
	return hex.EncodeToString(d[:])
}

// DestinationFromNodeKey derives a Reticulum destination hash from a QNTX
// node's ed25519 private key. This is identity convergence: the same key
// that signs attestations (did:key) addresses the node on the Reticulum mesh.
//
// Reticulum identities carry two public keys: X25519 (key exchange) and
// Ed25519 (signing). Both derive from the same seed. The destination hash is:
//
//	name_hash   = SHA-256("qntx.sync")
//	addr_hash   = SHA-256(name_hash + x25519_pub + ed25519_pub)
//	destination = addr_hash[:16]
func DestinationFromNodeKey(priv ed25519.PrivateKey) (Destination, error) {
	ed25519Pub := make([]byte, ed25519.PublicKeySize)
	copy(ed25519Pub, priv.Public().(ed25519.PublicKey))

	x25519Pub, err := x25519PublicFromEd25519Private(priv)
	if err != nil {
		return Destination{}, err
	}

	name := App + "." + SyncAspect

	// Step 1: hash the destination name
	nameHash := sha256.Sum256([]byte(name))

	// Step 2: SHA-256(name_hash + x25519_pub + ed25519_pub), truncate to 16 bytes
	// Reticulum orders public keys as: X25519 then Ed25519
	h := sha256.New()
	h.Write(nameHash[:])
	h.Write(x25519Pub)
	h.Write(ed25519Pub)

	var dest Destination
	copy(dest[:], h.Sum(nil)[:16])
	return dest, nil
}

// x25519PublicFromEd25519Private derives an X25519 public key from an
// Ed25519 private key. Uses the private-key path: SHA-512 the seed,
// clamp per RFC 7748, scalar-multiply by the Curve25519 base point.
//
// This avoids the Edwards-to-Montgomery birational map (which requires
// field arithmetic on the public key). The private key path is simpler
// and uses only standard library operations.
func x25519PublicFromEd25519Private(priv ed25519.PrivateKey) ([]byte, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return nil, errors.Newf("invalid ed25519 private key length: %d", len(priv))
	}

	// SHA-512 the 32-byte seed to get the expanded secret
	h := sha512.Sum512(priv.Seed())

	// Clamp per RFC 7748 section 5
	h[0] &= 248
	h[31] &= 127
	h[31] |= 64

	// Scalar multiply: x25519_pub = clamp(SHA-512(seed)[:32]) * G
	x25519Pub, err := curve25519.X25519(h[:32], curve25519.Basepoint)
	if err != nil {
		return nil, errors.Wrap(err, "x25519 scalar multiplication failed")
	}
	return x25519Pub, nil
}
