package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "A signature proves the key, and nothing reusable leaves the node."

const signedNodeOrigin = "https://node.example"

// signingNode is a node holding a grant for a key that never left its holder:
// the record names the DID and nothing that can be presented.
func signingNode(t *testing.T) (h *Handler, store *memTokenStore, key ed25519.PrivateKey, did string, id string) {
	t.Helper()
	store = newMemTokenStore()
	h = &Handler{users: &memUsers{}, sessions: newSessionStore(1), tokens: store, logger: testLogger()}
	h.SetIdentities([]string{mastodonAccount}, nil)
	h.configuredOrigin = signedNodeOrigin
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	did = EncodeDIDKey(key.Public().(ed25519.PublicKey))
	id, err = store.Issue(IssuedToken{Hash: signedOnly(did), DID: did, Label: "laptop", MintedBy: mastodonAccount,
		Level: LevelAttestor, Namespaces: []string{"ground"}, ExpiresAt: NeverEnds()})
	require.NoError(t, err)
	return h, store, key, did, id
}

// signature is what a holder of key sends: a fresh JWT for one request.
func signature(t *testing.T, key ed25519.PrivateKey, did, audience string, lasts time.Duration) string {
	t.Helper()
	jti := make([]byte, assertionIDBytes)
	_, err := rand.Read(jti)
	require.NoError(t, err)
	signed, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.RegisteredClaims{
		Issuer: did, Subject: did, Audience: jwt.ClaimStrings{audience},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(lasts)), ID: hex.EncodeToString(jti),
	}).SignedString(key)
	require.NoError(t, err)
	return signed
}

func presentedSigned(h *Handler, signed string) (Admission, bool) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", signedScheme+signed)
	return h.admissionOf(h.presented(req))
}

func TestASignatureIsAdmittedOnTheGrantItsDIDHolds(t *testing.T) {
	h, _, key, did, _ := signingNode(t)
	admitted, ok := presentedSigned(h, signature(t, key, did, signedNodeOrigin, time.Minute))
	require.True(t, ok, "a signature by the key a grant names was not admitted")
	assert.Equal(t, did, admitted.ActsAs())
	assert.Equal(t, mastodonAccount, admitted.Identity)
	assert.Equal(t, string(LevelAttestor), admitted.LevelName())
	assert.Equal(t, []string{"ground"}, admitted.Namespaces)
}

func TestASignatureIsAdmittedOnce(t *testing.T) {
	h, _, key, did, _ := signingNode(t)
	signed := signature(t, key, did, signedNodeOrigin, time.Minute)
	_, ok := presentedSigned(h, signed)
	require.True(t, ok)
	_, ok = presentedSigned(h, signed)
	assert.False(t, ok, "a signature presented again was admitted: it is reusable")
}

func TestASignatureForAnotherNodeIsRefused(t *testing.T) {
	h, _, key, did, _ := signingNode(t)
	_, ok := presentedSigned(h, signature(t, key, did, "https://elsewhere.example", time.Minute))
	assert.False(t, ok)
}

func TestASignatureLastingPastItsWindowIsRefused(t *testing.T) {
	h, _, key, did, _ := signingNode(t)
	_, ok := presentedSigned(h, signature(t, key, did, signedNodeOrigin, assertionMaxLife+time.Minute))
	assert.False(t, ok)
}

func TestASignatureByAnotherKeyIsRefused(t *testing.T) {
	h, _, _, did, _ := signingNode(t)
	_, other, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	_, ok := presentedSigned(h, signature(t, other, did, signedNodeOrigin, time.Minute))
	assert.False(t, ok)
}

func TestASignatureFromADIDNoGrantNamesIsRefused(t *testing.T) {
	h, _, _, _, _ := signingNode(t)
	_, stranger, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	strangerDID := EncodeDIDKey(stranger.Public().(ed25519.PublicKey))
	_, ok := presentedSigned(h, signature(t, stranger, strangerDID, signedNodeOrigin, time.Minute))
	assert.False(t, ok)
}

func TestASignatureOfASwitchedOffGrantIsRefused(t *testing.T) {
	h, store, key, did, id := signingNode(t)
	require.NoError(t, store.Revoke(id))
	_, ok := presentedSigned(h, signature(t, key, did, signedNodeOrigin, time.Minute))
	assert.False(t, ok)
}

// A grant held for a signature names nothing a bearer can present.
func TestAGrantHeldForASignatureAdmitsNoBearer(t *testing.T) {
	h, _, _, did, _ := signingNode(t)
	for _, presented := range []string{signedOnly(did), did, "signed:" + did} {
		_, ok := presentedAs(h, presented)
		assert.False(t, ok, presented)
	}
}

// 「[Opus 5.5] A request the gate admitted on a signature is the same caller when it is」
// 「[Opus 5.5] asked through the gate again inside the node (an A2A turn asks its sigil with」
// 「[Opus 5.5] the request it came in on): the grant it stood on goes with it, and the」
// 「[Opus 5.5] signature, presented once, is not presented again.」
func TestARequestTheGateAdmittedPassesItAgainAsTheSameCaller(t *testing.T) {
	h, _, key, did, _ := signingNode(t)
	var inside *http.Request
	outer := h.Middleware("/outer", everyLevel, func(_ http.ResponseWriter, r *http.Request) { inside = r })
	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.Header.Set("Authorization", signedScheme+signature(t, key, did, signedNodeOrigin, time.Minute))
	outer(httptest.NewRecorder(), req)
	require.NotNil(t, inside, "the signature was not admitted the first time")

	var again Admission
	rec := httptest.NewRecorder()
	h.Middleware("/inner", everyLevel, func(_ http.ResponseWriter, r *http.Request) {
		again, _ = AdmissionFrom(r.Context())
	})(rec, inside)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Equal(t, did, again.ActsAs())
	assert.Equal(t, string(LevelAttestor), again.LevelName())

	// What carries the grant is the gate's own pass: a new request with the
	// same signature is still the signature presented twice.
	replay := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	replay.Header = inside.Header.Clone()
	_, ok := h.admissionOf(h.presented(replay))
	assert.False(t, ok, "a signature presented again on a new request was admitted")
}
