package auth

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const publicDoorOrigin = "https://garden.example"

// publicDoor is a node with one door onto garden, one trusted binding signer,
// and nobody at all in auth.root_identities.
//
// No relying party is built for the door. A public registration never touches
// one — "the passkey would not be required for this particular path" — so a
// fixture that needed one would be testing something else.
func publicDoor(t *testing.T) (*Handler, ed25519.PrivateKey, *memUsers) {
	t.Helper()

	signerPub, signerPriv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	kept := &memUsers{}
	h := &Handler{
		logger:   testLogger(),
		users:    kept,
		sessions: newSessionStore(24),
	}
	h.SetIdentities(nil, []string{hex.EncodeToString(signerPub)})
	h.doors.set(map[string]*door{publicDoorOrigin: {namespace: "garden"}})
	return h, signerPriv, kept
}

// vouch is a provider having spoken: a binding signed by a key the node trusts,
// saying this browser key holds this account.
func vouch(t *testing.T, signer ed25519.PrivateKey, peerPub ed25519.PublicKey, provider, canonicalID, handle string) SignedBinding {
	t.Helper()

	b := SignedBinding{}
	b.Claim.PeerPubkeyHex = hex.EncodeToString(peerPub)
	b.Claim.Provider = provider
	b.Claim.CanonicalID = canonicalID
	b.Claim.IssuedAt = 1
	if handle != "" {
		b.Claim.Handle = &handle
	}
	b.SignatureHex = hex.EncodeToString(ed25519.Sign(signer, b.canonicalBytes()))
	b.SignerPubkeyHex = hex.EncodeToString(signer.Public().(ed25519.PublicKey))
	return b
}

// layeArrives walks the whole login: laye signs the challenge with the browser
// key and presents what the provider vouched for.
func layeArrives(t *testing.T, h *Handler, browser ed25519.PrivateKey, bindings []SignedBinding) *httptest.ResponseRecorder {
	t.Helper()

	challenge, err := h.layeChallenges.issue()
	require.NoError(t, err)

	did := EncodeDIDKey(browser.Public().(ed25519.PublicKey))
	body, err := json.Marshal(layeVerifyRequest{
		DID:       did,
		Challenge: challenge,
		Signature: base64.RawURLEncoding.EncodeToString(ed25519.Sign(browser, []byte(challenge))),
		Bindings:  bindings,
	})
	require.NoError(t, err)

	r := httptest.NewRequest(http.MethodPost, "/auth/laye/verify", bytes.NewReader(body))
	r.Header.Set("Origin", publicDoorOrigin)
	w := httptest.NewRecorder()
	h.handleLayeVerify(w, r)
	return w
}

// "YES ANYONE WHO CAN CLICK THE REGISTER BUTTON FOR GOOGLE OR META" — the
// provider is the gate and the only one.
func TestAStrangerTheProviderVouchedForIsLetIn(t *testing.T) {
	h, signer, kept := publicDoor(t)
	_, browser, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	w := layeArrives(t, h, browser, []SignedBinding{
		vouch(t, signer, browser.Public().(ed25519.PublicKey), "google", "google:110", "a@b.c"),
	})

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Len(t, kept.held, 1)
	assert.Equal(t, LevelPublicRegistration, kept.held[0].Level)
	assert.Equal(t, "garden", kept.held[0].Namespace)
	assert.Equal(t, []string{"a@b.c"}, kept.held[0].EmailAddresses)
}

// "the passkey would not be required for this particular path" — the session
// is issued here, with no device to go and fetch.
func TestAPublicRegistrationIsLoggedInWithoutADevice(t *testing.T) {
	h, signer, _ := publicDoor(t)
	_, browser, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	w := layeArrives(t, h, browser, []SignedBinding{
		vouch(t, signer, browser.Public().(ed25519.PublicKey), "google", "google:110", ""),
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var said map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &said))
	assert.Equal(t, "nothing", said["next"], "a public registration was sent at an authenticator")

	assert.NotEmpty(t, sessionOf(t, w), "no session was set")
}

// The session that comes out is PUBLIC_REGISTRATION, read off the User the
// node holds rather than asserted anywhere.
func TestThatSessionIsPublicRegistration(t *testing.T) {
	h, signer, _ := publicDoor(t)
	_, browser, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	w := layeArrives(t, h, browser, []SignedBinding{
		vouch(t, signer, browser.Public().(ed25519.PublicKey), "google", "google:110", ""),
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var seen Admission
	guarded := h.Middleware(func(_ http.ResponseWriter, r *http.Request) {
		seen, _ = AdmissionFrom(r.Context())
	})
	r := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionOf(t, w)})
	guarded(httptest.NewRecorder(), r)

	assert.Equal(t, LevelPublicRegistration, seen.Level)
	assert.NotEqual(t, LevelRoot, seen.Level, "a stranger came in as the node's owner")
}

// A binding nobody this node trusts signed is a stranger with a claim, and a
// claim is not a provider.
func TestABindingFromAnUntrustedSignerRegistersNobody(t *testing.T) {
	h, _, kept := publicDoor(t)
	_, stranger, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	_, browser, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	w := layeArrives(t, h, browser, []SignedBinding{
		vouch(t, stranger, browser.Public().(ed25519.PublicKey), "google", "google:110", ""),
	})

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Empty(t, kept.held)
}

// A registration belongs to the door it arrived at. An origin no door answers
// is not a door, so there is no namespace for one to belong to.
func TestArrivingWhereNoDoorAnswersRegistersNobody(t *testing.T) {
	h, signer, kept := publicDoor(t)
	_, browser, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	h.doors.set(map[string]*door{})

	w := layeArrives(t, h, browser, []SignedBinding{
		vouch(t, signer, browser.Public().(ed25519.PublicKey), "google", "google:110", ""),
	})

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Empty(t, kept.held)
}

// "when a user registers, we attest it" — registering is the thing that
// happens once, and logging in is the thing that happens every time.
func TestRegisteringIsAttestedOnceAndAdmissionEveryTime(t *testing.T) {
	h, signer, _ := publicDoor(t)
	wrote := &memAttestor{}
	h.SetAttestor(wrote)
	_, browser, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	binding := vouch(t, signer, browser.Public().(ed25519.PublicKey), "google", "google:110", "a@b.c")
	for range 2 {
		w := layeArrives(t, h, browser, []SignedBinding{binding})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	var registered, admitted int
	for _, as := range wrote.wrote {
		switch as.Predicates[0] {
		case PredicateRegistered:
			registered++
			assert.Equal(t, "garden", as.Attributes["door"])
			assert.Equal(t, "a@b.c", as.Attributes["handle"])
		case PredicateLoggedIn:
			admitted++
		}
	}
	assert.Equal(t, 1, registered, "logging in twice registered twice")
	assert.Equal(t, 2, admitted)
}

// Being listed is what makes ROOT, and this path never touches that list.
func TestAPublicRegistrationDoesNotBecomeRoot(t *testing.T) {
	h, signer, kept := publicDoor(t)
	_, browser, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	w := layeArrives(t, h, browser, []SignedBinding{
		vouch(t, signer, browser.Public().(ed25519.PublicKey), "google", "google:110", ""),
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	require.Len(t, kept.held, 1)
	assert.NotEqual(t, LevelRoot, kept.held[0].Level)
	assert.Empty(t, h.identities.roots(), "registering wrote somebody into root_identities")
}

func sessionOf(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range (&http.Response{Header: w.Header()}).Cookies() {
		if c.Name == sessionCookieName {
			return c.Value
		}
	}
	return ""
}
