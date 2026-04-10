package reticulum

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"
)

func TestDestinationFromNodeKey_Deterministic(t *testing.T) {
	// Fixed seed for reproducibility
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	priv := ed25519.NewKeyFromSeed(seed)

	dest1, err := DestinationFromNodeKey(priv)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	dest2, err := DestinationFromNodeKey(priv)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	if dest1 != dest2 {
		t.Fatalf("destination not deterministic: %s != %s", dest1, dest2)
	}

	// Destination should be 16 bytes (128 bits)
	if len(dest1) != 16 {
		t.Fatalf("destination length: got %d, want 16", len(dest1))
	}

	// Should produce a non-zero hash
	var zero Destination
	if dest1 == zero {
		t.Fatal("destination is all zeros")
	}

	t.Logf("destination hash: %s", dest1)
}

func TestDestinationFromNodeKey_DifferentKeys(t *testing.T) {
	seed1 := make([]byte, ed25519.SeedSize)
	seed2 := make([]byte, ed25519.SeedSize)
	seed2[0] = 1 // one bit different

	priv1 := ed25519.NewKeyFromSeed(seed1)
	priv2 := ed25519.NewKeyFromSeed(seed2)

	dest1, err := DestinationFromNodeKey(priv1)
	if err != nil {
		t.Fatalf("key1: %v", err)
	}

	dest2, err := DestinationFromNodeKey(priv2)
	if err != nil {
		t.Fatalf("key2: %v", err)
	}

	if dest1 == dest2 {
		t.Fatal("different keys produced same destination")
	}
}

func TestDestinationString(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	priv := ed25519.NewKeyFromSeed(seed)

	dest, err := DestinationFromNodeKey(priv)
	if err != nil {
		t.Fatal(err)
	}

	s := dest.String()
	if len(s) != 32 { // 16 bytes = 32 hex chars
		t.Fatalf("hex string length: got %d, want 32", len(s))
	}

	// Should round-trip through hex
	decoded, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex decode: %v", err)
	}
	if len(decoded) != 16 {
		t.Fatalf("decoded length: got %d, want 16", len(decoded))
	}
}

func TestX25519Derivation(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i * 3)
	}
	priv := ed25519.NewKeyFromSeed(seed)

	x25519Pub, err := x25519PublicFromEd25519Private(priv)
	if err != nil {
		t.Fatal(err)
	}

	if len(x25519Pub) != 32 {
		t.Fatalf("x25519 public key length: got %d, want 32", len(x25519Pub))
	}

	// Should be deterministic
	x25519Pub2, err := x25519PublicFromEd25519Private(priv)
	if err != nil {
		t.Fatal(err)
	}

	if hex.EncodeToString(x25519Pub) != hex.EncodeToString(x25519Pub2) {
		t.Fatal("x25519 derivation not deterministic")
	}

	// Should be non-zero
	allZero := true
	for _, b := range x25519Pub {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("x25519 public key is all zeros")
	}
}
