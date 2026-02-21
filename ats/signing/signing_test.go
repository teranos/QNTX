package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/mr-tron/base58"
	"github.com/teranos/QNTX/ats/types"
)

func testAttestation() *types.As {
	return &types.As{
		ID:         "AS-test-1234",
		Subjects:   []string{"alice"},
		Predicates: []string{"knows"},
		Contexts:   []string{"work"},
		Actors:     []string{"bob"},
		Timestamp:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Source:     "test",
	}
}

func testSigner() *Signer {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	did := encodeDIDKeyForTest(pub)
	return &Signer{PrivateKey: priv, DID: did}
}

// encodeDIDKeyForTest mirrors nodedid.encodeDIDKey
func encodeDIDKeyForTest(pub ed25519.PublicKey) string {
	buf := make([]byte, 2+len(pub))
	buf[0] = 0xed
	buf[1] = 0x01
	copy(buf[2:], pub)
	return "did:key:z" + base58.Encode(buf)
}

func TestSignAndVerify(t *testing.T) {
	signer := testSigner()
	as := testAttestation()

	if err := signer.Sign(as); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	if len(as.Signature) == 0 {
		t.Fatal("Signature should be populated after signing")
	}
	if as.SignerDID != signer.DID {
		t.Fatalf("SignerDID = %q, want %q", as.SignerDID, signer.DID)
	}

	if err := Verify(as); err != nil {
		t.Fatalf("Verify failed on valid signature: %v", err)
	}
}

func TestSignSkipsAlreadySigned(t *testing.T) {
	signer := testSigner()
	as := testAttestation()
	as.Signature = []byte("pre-existing")
	as.SignerDID = "did:key:zpre-existing"

	if err := signer.Sign(as); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	if string(as.Signature) != "pre-existing" {
		t.Fatal("Sign should not overwrite existing signature")
	}
}

func TestVerifyRejectsTampered(t *testing.T) {
	signer := testSigner()
	as := testAttestation()

	if err := signer.Sign(as); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	as.Subjects = []string{"mallory"}

	if err := Verify(as); err == nil {
		t.Fatal("Verify should reject tampered attestation")
	}
}

func TestVerifyUnsignedPassthrough(t *testing.T) {
	as := testAttestation()
	if err := Verify(as); err != nil {
		t.Fatalf("Verify should accept unsigned attestation, got: %v", err)
	}
}

func TestVerifySignatureWithoutDID(t *testing.T) {
	as := testAttestation()
	as.Signature = []byte("some-sig")

	if err := Verify(as); err == nil {
		t.Fatal("Verify should reject signature without signer DID")
	}
}

func TestCanonicalJSONDeterministic(t *testing.T) {
	as := testAttestation()

	b1, err := CanonicalJSON(as)
	if err != nil {
		t.Fatalf("CanonicalJSON failed: %v", err)
	}

	b2, err := CanonicalJSON(as)
	if err != nil {
		t.Fatalf("CanonicalJSON failed: %v", err)
	}

	if string(b1) != string(b2) {
		t.Fatalf("CanonicalJSON not deterministic:\n  %s\n  %s", b1, b2)
	}
}

func TestCanonicalJSONExcludesCreatedAt(t *testing.T) {
	as1 := testAttestation()
	as1.CreatedAt = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	as2 := testAttestation()
	as2.CreatedAt = time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)

	b1, _ := CanonicalJSON(as1)
	b2, _ := CanonicalJSON(as2)

	if string(b1) != string(b2) {
		t.Fatalf("CanonicalJSON should not include created_at:\n  %s\n  %s", b1, b2)
	}
}

func TestDecodeDIDKeyRoundTrip(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	did := encodeDIDKeyForTest(pub)

	decoded, err := DecodeDIDKey(did)
	if err != nil {
		t.Fatalf("DecodeDIDKey failed: %v", err)
	}

	if !pub.Equal(decoded) {
		t.Fatal("DecodeDIDKey round-trip failed: keys don't match")
	}
}

func TestDecodeDIDKeyInvalidFormat(t *testing.T) {
	_, err := DecodeDIDKey("not-a-did")
	if err == nil {
		t.Fatal("DecodeDIDKey should reject invalid format")
	}
}
