package auth

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An authenticator hands back the user handle a passkey was made under, and
// the node checks it against the handle it kept for that passkey (§7.2 step 6).

const gardenOrigin = "https://portal.garden.test"

// authenticator is one ES256 passkey, able to answer a sign-in.
type authenticator struct {
	id  []byte
	key *ecdsa.PrivateKey
}

func newAuthenticator(t *testing.T, id string) authenticator {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return authenticator{id: []byte(id), key: key}
}

// credential is how the node keeps this passkey: its id and COSE public key.
func (a authenticator) credential(t *testing.T) webauthn.Credential {
	t.Helper()
	cose, err := cbor.Marshal(map[int]any{
		1: 2, 3: -7, -1: 1,
		-2: a.key.PublicKey.X.FillBytes(make([]byte, 32)),
		-3: a.key.PublicKey.Y.FillBytes(make([]byte, 32)),
	})
	require.NoError(t, err)
	return webauthn.Credential{ID: a.id, PublicKey: cose, AttestationType: "none"}
}

// answer signs authenticatorData || SHA-256(clientDataJSON) for the challenge,
// handing back userHandle (§7.2 step 21).
func (a authenticator) answer(t *testing.T, rpID, challenge, userHandle string) *protocol.ParsedCredentialAssertionData {
	t.Helper()
	rpIDHash := sha256.Sum256([]byte(rpID))
	authData := append(rpIDHash[:], 0x01, 0, 0, 0, 1)
	clientData, err := json.Marshal(map[string]any{"type": "webauthn.get", "challenge": challenge, "origin": gardenOrigin})
	require.NoError(t, err)
	clientHash := sha256.Sum256(clientData)
	signed := sha256.Sum256(append(append([]byte{}, authData...), clientHash[:]...))
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, signed[:])
	require.NoError(t, err)

	body, err := json.Marshal(map[string]any{
		"id":    base64.RawURLEncoding.EncodeToString(a.id),
		"rawId": base64.RawURLEncoding.EncodeToString(a.id),
		"type":  "public-key",
		"response": map[string]any{
			"authenticatorData": base64.RawURLEncoding.EncodeToString(authData),
			"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientData),
			"signature":         base64.RawURLEncoding.EncodeToString(signature),
			"userHandle":        base64.RawURLEncoding.EncodeToString([]byte(userHandle)),
		},
	})
	require.NoError(t, err)
	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(body))
	require.NoError(t, err)
	return parsed
}

// beginSignIn begins a login at the garden door for one half-admitted
// browser, and gives back its cookie and the challenge it was issued.
func beginSignIn(t *testing.T, h *Handler) (*http.Cookie, string) {
	t.Helper()
	browser := halfAdmitted(t, h)
	r := httptest.NewRequest(http.MethodPost, "/auth/login/begin", nil)
	r.Header.Set("Origin", gardenOrigin)
	r.AddCookie(browser)
	begun := httptest.NewRecorder()
	h.handleLoginBegin(begun, r)
	require.Equal(t, http.StatusOK, begun.Code, begun.Body.String())
	var options struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
		} `json:"publicKey"`
	}
	require.NoError(t, json.Unmarshal(begun.Body.Bytes(), &options))
	return browser, options.PublicKey.Challenge
}

// answeringASignIn begins a login at the garden door and answers it with a.
func answeringASignIn(t *testing.T, h *Handler, a authenticator, userHandle string) error {
	t.Helper()
	browser, challenge := beginSignIn(t, h)
	session, ok := h.ceremonies.finish(signingIn(Presented{pendingToken: browser.Value}))
	require.True(t, ok)

	r := httptest.NewRequest(http.MethodPost, "/auth/login/finish", nil)
	r.Header.Set("Origin", gardenOrigin)
	arrived, ok := h.doorFor(r)
	require.True(t, ok)
	creds, err := h.devicesOf(arrived.namespace, mastodonAccount)
	require.NoError(t, err)

	_, err = h.validateAssertion(arrived.rp, *session, a.answer(t, "garden.test", challenge, userHandle), creds)
	return err
}

// A person holds passkeys made under qntx-owner before handles were theirs,
// and under their User id since. Both sign in, each under its own handle.
func TestAPasskeySignsInUnderTheHandleItWasMadeUnder(t *testing.T) {
	h := handlerWithDoors(t, garden())
	h.users = &memUsers{held: []User{{ID: "US-ONE", Level: LevelRoot, Accounts: []UserAccount{{Provider: "mastodon", CanonicalID: mastodonAccount}}}}}
	laptop, phone := newAuthenticator(t, "laptop"), newAuthenticator(t, "phone")
	require.NoError(t, h.creds.saveAt(laptop.credential(t), "did:key:zlaptop", mastodonAccount, "garden", "US-ONE"))
	require.NoError(t, h.creds.saveAt(phone.credential(t), "did:key:zphone", mastodonAccount, "garden", legacyUserHandle))

	assert.NoError(t, answeringASignIn(t, h, laptop, "US-ONE"), "a passkey made under the User's id")
	assert.NoError(t, answeringASignIn(t, h, phone, legacyUserHandle), "a passkey made before handles were the User's")
	assert.Error(t, answeringASignIn(t, h, laptop, legacyUserHandle), "a handle the passkey was not made under")
}
