package server

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/teranos/QNTX/ats"
	"github.com/teranos/QNTX/ats/signing"
	"github.com/teranos/QNTX/ats/types"
	"github.com/teranos/QNTX/internal/access"
	"github.com/teranos/QNTX/server/auth"
	"go.uber.org/zap"
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

const timsAccount = "https://mastodon.example/@tim"

// personSession is Tim, admitted by his account and reaching his User.
func personSession() *auth.Admission {
	session := auth.Admitted(auth.LevelSuper)
	session.Identity = timsAccount
	session.UserID = "UStim"
	return &session
}

// signingAs posts as writingAs does, on a node that keeps Tim, who holds keys.
func signingAs(t *testing.T, caller *auth.Admission, body string, keys ...string) (ats.AttestationStore, *httptest.ResponseRecorder) {
	t.Helper()
	s, store := writingServer(t)
	users, _, err := auth.OpenUserTable(s.nodeDB, nil)
	if err != nil {
		t.Fatalf("OpenUserTable: %v", err)
	}
	tim := auth.User{ID: "UStim", Level: auth.LevelSuper}
	for _, did := range keys {
		tim.Keys = append(tim.Keys, auth.UserKey{DID: did, Origin: auth.OriginBrowser})
	}
	if err := users.Put(tim); err != nil {
		t.Fatalf("Put: %v", err)
	}
	s.authHandler, err = auth.New(nil, "localhost", nil, 8770, 8820, 24, zap.NewNop().Sugar(),
		func(next http.HandlerFunc) http.HandlerFunc { return next },
		nil, users, false, []string{timsAccount}, nil)
	if err != nil {
		t.Fatalf("auth.New: %v", err)
	}
	return store, postAs(s, caller, body)
}

// The node signs nothing on a writer's behalf: what the browser signed is
// stored as it signed it, and still verifies when read back.
func TestASignedAttestationIsStoredAsSigned(t *testing.T) {
	body, did := signedBody(t, []string{"tim"}, nil)
	store, rec := signingAs(t, personSession(), body, did)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	found, err := store.GetAttestations(ats.AttestationFilter{TimeStart: types.Unbounded(), TimeEnd: types.Unbounded(), Limit: 1})
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

// "this person wrote it": a key the person admitted does not hold is not
// theirs to have signed with, however good the signature.
func TestASignatureByAKeyThePersonDoesNotHoldIsRefused(t *testing.T) {
	body, _ := signedBody(t, []string{"tim"}, nil)
	_, rec := signingAs(t, personSession(), body, "did:key:zsomeoneelse")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}

func TestASignedAttestationFromNoUserIsRefused(t *testing.T) {
	body, did := signedBody(t, []string{"tim"}, nil)
	stranger := personSession()
	stranger.UserID = "USnobody"
	_, rec := signingAs(t, stranger, body, did)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}

// A node without login admits nobody, so nobody is named to have signed.
func TestASignedAttestationOnANodeWithoutLoginIsRefused(t *testing.T) {
	body, _ := signedBody(t, []string{"tim"}, nil)
	_, rec := writingAs(t, nil, body)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}

// A bearer token names the route that minted it and no User, so nobody is
// named to hold the key.
func TestASignedAttestationThroughATokenIsRefused(t *testing.T) {
	body, did := signedBody(t, []string{tokenDID, "ground"}, nil)
	_, rec := signingAs(t, tokenAdmission(), body, did)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}

func TestASignatureNotOverWhatCameIsRefused(t *testing.T) {
	body, did := signedBody(t, []string{"tim"}, func(b map[string]any) { b["source"] = "elsewhere" })
	_, rec := signingAs(t, personSession(), body, did)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestASignerDIDThatNamesNoKeyIsRefused(t *testing.T) {
	const notAKey = "did:web:example.com"
	body, _ := signedBody(t, []string{"tim"}, func(b map[string]any) { b["signer_did"] = notAKey })
	_, rec := signingAs(t, personSession(), body, notAKey)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

// A token that already names its own DID first is not given it twice.
func TestATokenNamingItselfFirstIsNamedOnce(t *testing.T) {
	store, rec := writingAs(t, tokenAdmission(),
		`{"subjects":["qntx"],"predicates":["noted"],"actors":["`+tokenDID+`","ground"]}`)

	actors := actorsOf(t, store, rec)
	if !slices.Equal(actors, []string{tokenDID, "ground"}) {
		t.Fatalf("actors = %v, want the token's DID once, leading", actors)
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
			body, did := signedBody(t, actors, edit)
			_, rec := signingAs(t, personSession(), body, did)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400: %s", rec.Code, rec.Body.String())
			}
		})
	}
}
