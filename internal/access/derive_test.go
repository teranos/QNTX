package access

import (
	"crypto/ed25519"
	"testing"
)

func keyFrom(t *testing.T, fill byte) ed25519.PrivateKey {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = fill
	}
	return ed25519.NewKeyFromSeed(seed)
}

func didOf(k ed25519.PrivateKey) string {
	return EncodeDIDKey(k.Public().(ed25519.PublicKey))
}

// A key derived for a purpose is the same key every time it is derived, so
// what holds it keeps nothing more to be the same DID tomorrow.
func TestADerivedKeyIsTheSameEveryTime(t *testing.T) {
	node := keyFrom(t, 7)
	key, err := DeriveKey(node, "agent:root")
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}
	again, err := DeriveKey(node, "agent:root")
	if err != nil {
		t.Fatalf("DeriveKey again: %v", err)
	}
	if !key.Equal(again) {
		t.Errorf("derived twice, it is two keys: %s and %s", didOf(key), didOf(again))
	}
}

// What holds a derived key signs as its own DID, which is not the key's it was
// derived from.
func TestADerivedKeySignsAsItsOwnDID(t *testing.T) {
	node := keyFrom(t, 7)
	key, err := DeriveKey(node, "agent:root")
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}
	if didOf(key) == didOf(node) {
		t.Errorf("the derived DID is the key's own: what is derived is itself, not what it was derived from")
	}
	named, err := DecodeUserDID(didOf(key))
	if err != nil {
		t.Fatalf("DecodeUserDID: %v", err)
	}
	if !ed25519.Verify(named, []byte("said"), ed25519.Sign(key, []byte("said"))) {
		t.Errorf("what the derived key signed does not verify under its own DID")
	}
}

// What it is derived for and what it is derived from both decide it.
func TestADerivedKeyIsItsPurposesAndItsKeys(t *testing.T) {
	root, err := DeriveKey(keyFrom(t, 7), "agent:root")
	if err != nil {
		t.Fatal(err)
	}
	other, err := DeriveKey(keyFrom(t, 7), "agent:other")
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := DeriveKey(keyFrom(t, 8), "agent:root")
	if err != nil {
		t.Fatal(err)
	}
	if didOf(root) == didOf(other) {
		t.Errorf("two purposes derived one key")
	}
	if didOf(root) == didOf(elsewhere) {
		t.Errorf("two keys derived one key")
	}
	if _, err := DeriveKey(keyFrom(t, 7), ""); err == nil {
		t.Errorf("a key was derived for no purpose")
	}
	if _, err := DeriveKey(nil, "agent:root"); err == nil {
		t.Errorf("a key was derived from no key")
	}
}
