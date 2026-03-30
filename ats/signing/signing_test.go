package signing

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"testing"
	"time"

	"github.com/mr-tron/base58"
	"github.com/teranos/QNTX/ats/types"
	"google.golang.org/protobuf/types/known/structpb"
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
	return NewSigner(priv, did)
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

// TestSignVerifyJSONRoundTrip simulates the full sign → DB store → wire → verify path.
// Attributes are marshaled to JSON (as in the Rust FFI boundary) and unmarshaled back,
// which can change types (e.g. nested structures). The canonical JSON must match.
func TestSignVerifyJSONRoundTrip(t *testing.T) {
	signer := testSigner()
	as := &types.As{
		ID:         "AS-MODELAMA-WEAVE-TEST-ABCD1234",
		Subjects:   []string{"model:llama-3.2-1b"},
		Predicates: []string{"Weave"},
		Contexts:   []string{"test-ctx"},
		Actors:     []string{"AS-MODELAMA-WEAVE-TEST-ABCD1234"},
		Timestamp:  time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
		Source:     "llama-cpp",
		Attributes: map[string]interface{}{
			"prompt":          "hello",
			"text":            "world",
			"model":           "llama-3.2-1b",
			"token_count":     float64(5),
			"mean_confidence": float64(0.95),
			"mean_entropy":    float64(2.3),
			"weave_source":    "llama-cpp",
			"source_version":  "0.18.1",
			"tokens": []interface{}{
				map[string]interface{}{
					"text":       "hello",
					"position":   float64(0),
					"confidence": float64(0.9),
					"entropy":    float64(1.5),
					"top_gap":    float64(0.3),
					"top_k": []interface{}{
						map[string]interface{}{"text": "hello", "prob": float64(0.9)},
						map[string]interface{}{"text": "world", "prob": float64(0.1)},
					},
				},
			},
		},
	}

	// Sign
	if err := signer.Sign(as); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	signedCanonical, _ := CanonicalJSON(as)

	// Simulate DB roundtrip: marshal to JSON, unmarshal back (like fromRustJSON)
	type ffiAttestation struct {
		ID         string                 `json:"id,omitempty"`
		Subjects   []string               `json:"subjects,omitempty"`
		Predicates []string               `json:"predicates,omitempty"`
		Contexts   []string               `json:"contexts,omitempty"`
		Actors     []string               `json:"actors,omitempty"`
		Timestamp  int64                  `json:"timestamp,omitempty"`
		Source     string                 `json:"source,omitempty"`
		Attributes map[string]interface{} `json:"attributes,omitempty"`
		CreatedAt  int64                  `json:"created_at,omitempty"`
		Signature  []byte                 `json:"signature,omitempty"`
		SignerDID  string                 `json:"signer_did,omitempty"`
	}

	ffi := ffiAttestation{
		ID:         as.ID,
		Subjects:   as.Subjects,
		Predicates: as.Predicates,
		Contexts:   as.Contexts,
		Actors:     as.Actors,
		Timestamp:  as.Timestamp.UnixMilli(),
		Source:     as.Source,
		Attributes: as.Attributes,
		CreatedAt:  as.CreatedAt.UnixMilli(),
		Signature:  as.Signature,
		SignerDID:  as.SignerDID,
	}
	jsonBytes, err := json.Marshal(ffi)
	if err != nil {
		t.Fatalf("marshal to FFI JSON failed: %v", err)
	}

	var ffi2 ffiAttestation
	if err := json.Unmarshal(jsonBytes, &ffi2); err != nil {
		t.Fatalf("unmarshal from FFI JSON failed: %v", err)
	}

	roundTripped := &types.As{
		ID:         ffi2.ID,
		Subjects:   ffi2.Subjects,
		Predicates: ffi2.Predicates,
		Contexts:   ffi2.Contexts,
		Actors:     ffi2.Actors,
		Timestamp:  time.UnixMilli(ffi2.Timestamp),
		Source:     ffi2.Source,
		Attributes: ffi2.Attributes,
		CreatedAt:  time.UnixMilli(ffi2.CreatedAt),
		Signature:  ffi2.Signature,
		SignerDID:  ffi2.SignerDID,
	}

	// Verify after roundtrip
	roundTrippedCanonical, _ := CanonicalJSON(roundTripped)
	if string(signedCanonical) != string(roundTrippedCanonical) {
		t.Fatalf("canonical JSON differs after DB roundtrip:\n  sign: %s\n  verify: %s", signedCanonical, roundTrippedCanonical)
	}

	if err := Verify(roundTripped); err != nil {
		t.Fatalf("Verify failed after DB roundtrip: %v", err)
	}
}

// TestSignVerifyProtoRoundTrip simulates the full sign → DB → proto wire → verify path
// including the structpb.Struct conversion that happens during sync.
func TestSignVerifyProtoRoundTrip(t *testing.T) {
	signer := testSigner()
	as := &types.As{
		ID:         "AS-MODELAMA-WEAVE-TEST-EFGH5678",
		Subjects:   []string{"model:llama-3.2-1b"},
		Predicates: []string{"Weave"},
		Contexts:   []string{"test-ctx"},
		Actors:     []string{"AS-MODELAMA-WEAVE-TEST-EFGH5678"},
		Timestamp:  time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
		Source:     "llama-cpp",
		Attributes: map[string]interface{}{
			"prompt":          "hello",
			"text":            "world",
			"model":           "llama-3.2-1b",
			"token_count":     float64(5),
			"mean_confidence": float64(0.95),
			"mean_entropy":    float64(2.3),
			"weave_source":    "llama-cpp",
			"source_version":  "0.18.1",
			"tokens": []interface{}{
				map[string]interface{}{
					"text":       "hello",
					"position":   float64(0),
					"confidence": float64(0.9),
					"entropy":    float64(1.5),
					"top_gap":    float64(0.3),
					"top_k": []interface{}{
						map[string]interface{}{"text": "hello", "prob": float64(0.9)},
						map[string]interface{}{"text": "world", "prob": float64(0.1)},
					},
				},
			},
		},
	}

	// Sign
	if err := signer.Sign(as); err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	signedCanonical, _ := CanonicalJSON(as)

	// Simulate proto wire roundtrip: map → structpb.Struct → JSON → structpb.Struct → map
	attrs, err := structpb.NewStruct(as.Attributes)
	if err != nil {
		t.Fatalf("structpb.NewStruct failed: %v", err)
	}

	// Marshal via structpb JSON (what happens during WebSocket sync)
	attrJSON, err := attrs.MarshalJSON()
	if err != nil {
		t.Fatalf("structpb MarshalJSON failed: %v", err)
	}

	// Unmarshal back
	var attrs2 structpb.Struct
	if err := attrs2.UnmarshalJSON(attrJSON); err != nil {
		t.Fatalf("structpb UnmarshalJSON failed: %v", err)
	}

	// Convert back to Go map (like ToTypes does)
	roundTripped := &types.As{
		ID:         as.ID,
		Subjects:   as.Subjects,
		Predicates: as.Predicates,
		Contexts:   as.Contexts,
		Actors:     as.Actors,
		Timestamp:  as.Timestamp,
		Source:     as.Source,
		Attributes: attrs2.AsMap(),
		Signature:  as.Signature,
		SignerDID:  as.SignerDID,
	}

	roundTrippedCanonical, _ := CanonicalJSON(roundTripped)
	if string(signedCanonical) != string(roundTrippedCanonical) {
		t.Fatalf("canonical JSON differs after proto roundtrip:\n  sign: %s\n  verify: %s", signedCanonical, roundTrippedCanonical)
	}

	if err := Verify(roundTripped); err != nil {
		t.Fatalf("Verify failed after proto roundtrip: %v", err)
	}
}
