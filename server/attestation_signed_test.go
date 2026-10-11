package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/signing"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/access"
	"github.com/teranos/QNTX/server/auth"
)

// signedBody is an attestation the way a browser sends one it signed: the
// signature over the bytes the node will verify, timestamp in seconds.
func signedBody(t *testing.T, actors []string, edit func(map[string]any)) (string, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	did := access.EncodeDIDKey(pub)
	as := &types.As{
		ID:         "AS-SIGNED-IN-THE-BROWSER",
		Subjects:   []string{"qntx"},
		Predicates: []string{"noted"},
		Contexts:   []string{"_"},
		Actors:     actors,
		Timestamp:  time.Unix(1735689600, 0),
		Source:     "ts-element",
		Attributes: map[string]any{"said": "<offline>"},
	}
	if err := signing.NewSigner(priv, did).Sign(as); err != nil {
		t.Fatalf("Sign: %v", err)
	}
	body := map[string]any{
		"id":         as.ID,
		"subjects":   as.Subjects,
		"predicates": as.Predicates,
		"contexts":   as.Contexts,
		"actors":     as.Actors,
		"timestamp":  as.Timestamp.Unix(),
		"source":     as.Source,
		"attributes": as.Attributes,
		"signature":  base64.StdEncoding.EncodeToString(as.Signature),
		"signer_did": did,
	}
	if edit != nil {
		edit(body)
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return string(raw), did
}

func personSession() *auth.Admission {
	session := auth.Admitted(auth.LevelSuper)
	session.Identity = "https://mastodon.example/@tim"
	return &session
}

// The node signs nothing on a writer's behalf: what the browser signed is
// stored as it signed it, and still verifies when read back.
func TestASignedAttestationIsStoredAsSigned(t *testing.T) {
	body, did := signedBody(t, []string{"tim"}, nil)
	store, rec := writingAs(t, personSession(), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	found, err := store.GetAttestations(ats.AttestationFilter{Limit: 1})
	if err != nil || len(found) != 1 {
		t.Fatalf("read back %v, %v", found, err)
	}
	if found[0].SignerDID != did {
		t.Fatalf("signer_did = %q, want the browser's %q", found[0].SignerDID, did)
	}
	if err := signing.Verify(found[0]); err != nil {
		t.Fatalf("what was stored no longer verifies: %v", err)
	}
}

func TestASignatureNotOverWhatCameIsRefused(t *testing.T) {
	body, _ := signedBody(t, []string{"tim"}, func(b map[string]any) { b["source"] = "elsewhere" })
	_, rec := writingAs(t, personSession(), body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestASignerDIDThatNamesNoKeyIsRefused(t *testing.T) {
	body, _ := signedBody(t, []string{"tim"}, func(b map[string]any) { b["signer_did"] = "did:web:example.com" })
	_, rec := writingAs(t, personSession(), body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// A token is its own actor, and the node would put its DID first. Under a
// signature it cannot, so the signer is told to name it.
func TestASignedAttestationMissingTheTokenIsRefused(t *testing.T) {
	body, _ := signedBody(t, []string{"ground"}, nil)
	_, rec := writingAs(t, tokenAdmission(), body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), tokenDID) {
		t.Fatalf("the refusal does not name the actor the signer must include: %s", rec.Body.String())
	}
}

func TestASignedAttestationNamingTheTokenFirstIsStored(t *testing.T) {
	body, _ := signedBody(t, []string{tokenDID, "ground"}, nil)
	_, rec := writingAs(t, tokenAdmission(), body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
}

// The node fills in no actor, id or moment under a signature.
func TestASignedAttestationNamesWhatTheNodeWouldFillIn(t *testing.T) {
	for name, edit := range map[string]func(map[string]any){
		"no actors":     func(b map[string]any) { b["actors"] = []string{} },
		"no id":         func(b map[string]any) { delete(b, "id") },
		"no timestamp":  func(b map[string]any) { delete(b, "timestamp") },
		"no signer_did": func(b map[string]any) { delete(b, "signer_did") },
		"no signature":  func(b map[string]any) { delete(b, "signature") },
	} {
		t.Run(name, func(t *testing.T) {
			actors := []string{"tim"}
			if name == "no actors" {
				actors = []string{}
			}
			body, _ := signedBody(t, actors, edit)
			_, rec := writingAs(t, personSession(), body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
		})
	}
}
