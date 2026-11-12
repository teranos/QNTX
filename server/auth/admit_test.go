package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "I WANT A SESSION TO BE ABLE TO NAME ITSELF"

// saidYesTo is a node holding a key the person said yes to at the passkey, as
// device.go records one: its own client, signing as itself.
func saidYesTo(t *testing.T) (h *Handler, store *memTokenStore, key ed25519.PrivateKey, did string) {
	t.Helper()
	store = newMemTokenStore()
	h = &Handler{users: &memUsers{}, sessions: newSessionStore(1), tokens: store, logger: testLogger()}
	h.SetIdentities([]string{mastodonAccount}, nil)
	h.configuredOrigin = signedNodeOrigin
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	did = EncodeDIDKey(key.Public().(ed25519.PublicKey))
	until := time.Now().Add(time.Hour)
	_, err = store.Issue(IssuedToken{Hash: signedOnly(did), DID: did, Label: "ground l", MintedBy: mastodonAccount,
		Level: LevelRoot, Namespaces: []string{"ground"}, ExpiresAt: &until, ClientDID: did})
	require.NoError(t, err)
	return h, store, key, did
}

// aSessionsKey is a key made where a session starts.
func aSessionsKey(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return key, EncodeDIDKey(key.Public().(ed25519.PublicKey))
}

// admit asks /auth/admit for did under label, signed by key as signer.
func admit(h *Handler, key ed25519.PrivateKey, signer, did, label string, t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"did": {did}, "label": {label}}
	req := httptest.NewRequest(http.MethodPost, admitPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", signedScheme+signature(t, key, signer, signedNodeOrigin, time.Minute))
	w := httptest.NewRecorder()
	h.Routes()[admitPath](w, req)
	return w
}

func TestAKeySaidYesToAdmitsASessionsKey(t *testing.T) {
	h, _, ground, groundDID := saidYesTo(t)
	sessionKey, sessionDID := aSessionsKey(t)

	w := admit(h, ground, groundDID, sessionDID, "Alder", t)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"token_type":"Signed"`)

	// The session signs as itself, and is its own DID: what i name names.
	admitted, ok := presentedSigned(h, signature(t, sessionKey, sessionDID, signedNodeOrigin, time.Minute))
	require.True(t, ok, "the session's key is not admitted")
	assert.Nil(t, admitted.Grant)
	assert.Equal(t, sessionDID, admitted.TokenDID)
	assert.Equal(t, sessionDID, admitted.ClientDID)
	assert.Equal(t, "Alder", admitted.TokenLabel)
	assert.Equal(t, mastodonAccount, admitted.Identity)
	assert.Equal(t, string(LevelRoot), admitted.LevelName())
}

func TestASessionsKeyAdmitsNoKeyOfItsOwn(t *testing.T) {
	h, _, ground, groundDID := saidYesTo(t)
	sessionKey, sessionDID := aSessionsKey(t)
	require.Equal(t, http.StatusOK, admit(h, ground, groundDID, sessionDID, "Alder", t).Code)

	// 「[Fable 5.1] A session's key is its own client too, so it admits as the said-yes key does:」
	// 「[Fable 5.1] the chain is the person's yes, carried on. What it admits ends when it ends.」
	_, grandchildDID := aSessionsKey(t)
	w := admit(h, sessionKey, sessionDID, grandchildDID, "Slate", t)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestAKeyNobodySaidYesToAdmitsNone(t *testing.T) {
	h, _, _, _ := saidYesTo(t)
	stranger, strangerDID := aSessionsKey(t)
	_, sessionDID := aSessionsKey(t)
	w := admit(h, stranger, strangerDID, sessionDID, "Alder", t)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

func TestAHeldKeyIsNotAdmittedAgain(t *testing.T) {
	h, _, ground, groundDID := saidYesTo(t)
	_, sessionDID := aSessionsKey(t)
	require.Equal(t, http.StatusOK, admit(h, ground, groundDID, sessionDID, "Alder", t).Code)
	assert.Equal(t, http.StatusConflict, admit(h, ground, groundDID, sessionDID, "Alder", t).Code)
	assert.Equal(t, http.StatusBadRequest, admit(h, ground, groundDID, groundDID, "Alder", t).Code, "a key admitted itself")
}

func TestASessionsKeyEndsWithTheAdmittingKeys(t *testing.T) {
	h, store, ground, groundDID := saidYesTo(t)
	_, sessionDID := aSessionsKey(t)
	require.Equal(t, http.StatusOK, admit(h, ground, groundDID, sessionDID, "Alder", t).Code)
	listed, err := store.List()
	require.NoError(t, err)
	byDID := map[string]TokenInfo{}
	for _, held := range listed {
		byDID[held.DID] = held
	}
	require.Contains(t, byDID, sessionDID)
	assert.Equal(t, *byDID[groundDID].ExpiresAt, *byDID[sessionDID].ExpiresAt)
}
