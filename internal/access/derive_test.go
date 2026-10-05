package access

import (
	"crypto/ed25519"
	"encoding/hex"
	"strings"
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

// A token derived from a key is the same token every time it is derived, so
// what holds the key needs to keep nothing more to be the same DID tomorrow.
func TestADerivedTokenIsTheSameEveryTime(t *testing.T) {
	node := keyFrom(t, 7)
	raw, did, err := DeriveToken(node, "agent:root")
	if err != nil {
		t.Fatalf("DeriveToken: %v", err)
	}
	again, didAgain, err := DeriveToken(node, "agent:root")
	if err != nil {
		t.Fatalf("DeriveToken again: %v", err)
	}
	if raw != again || did != didAgain {
		t.Errorf("derived twice, it is two tokens: %s and %s", did, didAgain)
	}
}

// It is a token like any other: the prefix, an ed25519 seed in hex, and the
// DID that seed names.
func TestADerivedTokenIsATokenWithItsOwnDID(t *testing.T) {
	node := keyFrom(t, 7)
	raw, did, err := DeriveToken(node, "agent:root")
	if err != nil {
		t.Fatalf("DeriveToken: %v", err)
	}
	body, prefixed := strings.CutPrefix(raw, TokenPrefix)
	if !prefixed || len(body) != TokenSeedBytes*2 {
		t.Fatalf("the derived token is not shaped as one: %d characters after the prefix", len(body))
	}
	seed, err := hex.DecodeString(body)
	if err != nil {
		t.Fatalf("the derived token is not hex: %v", err)
	}
	pub, ok := ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
	if !ok || EncodeDIDKey(pub) != did {
		t.Errorf("the DID is not the one the token's seed names")
	}
	nodePub, _ := node.Public().(ed25519.PublicKey)
	if did == EncodeDIDKey(nodePub) {
		t.Errorf("the derived DID is the key's own: what is derived is itself, not what it was derived from")
	}
	if strings.Contains(raw, hex.EncodeToString(node.Seed())) {
		t.Errorf("the derived token carries the key it was derived from")
	}
}

// What holds a derived token signs as its DID: the key is the one the token
// is the seed of.
func TestADerivedKeySignsAsTheDerivedTokensDID(t *testing.T) {
	node := keyFrom(t, 7)
	_, did, err := DeriveToken(node, "agent:root")
	if err != nil {
		t.Fatalf("DeriveToken: %v", err)
	}
	key, err := DeriveKey(node, "agent:root")
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}
	pub, ok := key.Public().(ed25519.PublicKey)
	if !ok || EncodeDIDKey(pub) != did {
		t.Fatalf("the derived key is not the derived token's")
	}
	signed := ed25519.Sign(key, []byte("said"))
	named, err := DecodeUserDID(did)
	if err != nil {
		t.Fatalf("DecodeUserDID: %v", err)
	}
	if !ed25519.Verify(named, []byte("said"), signed) {
		t.Errorf("what the derived key signed does not verify under the derived DID")
	}
}

// What it is derived for and what it is derived from both decide it.
func TestADerivedTokenIsItsPurposesAndItsKeys(t *testing.T) {
	root, rootDID, err := DeriveToken(keyFrom(t, 7), "agent:root")
	if err != nil {
		t.Fatal(err)
	}
	other, otherDID, err := DeriveToken(keyFrom(t, 7), "agent:other")
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, elsewhereDID, err := DeriveToken(keyFrom(t, 8), "agent:root")
	if err != nil {
		t.Fatal(err)
	}
	if root == other || rootDID == otherDID {
		t.Errorf("two purposes derived one token")
	}
	if root == elsewhere || rootDID == elsewhereDID {
		t.Errorf("two keys derived one token")
	}
	if _, _, err := DeriveToken(keyFrom(t, 7), ""); err == nil {
		t.Errorf("a token was derived for no purpose")
	}
	if _, _, err := DeriveToken(nil, "agent:root"); err == nil {
		t.Errorf("a token was derived from no key")
	}
}
