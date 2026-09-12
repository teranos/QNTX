package auth

import (
	"crypto/ed25519"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A half-admission that a trusted signer vouched for, as laye leaves it.
func vouchedHalfAdmission(t *testing.T, h *Handler) (halfAdmission, ed25519.PrivateKey) {
	t.Helper()
	signerPub, signer, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	peerPub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	h.SetIdentities([]string{mastodonAccount}, []string{hex.EncodeToString(signerPub)})
	binding := vouch(t, signer, peerPub, "mastodon", mastodonAccount, "@tim@mastodon.example")
	return halfAdmission{identity: mastodonAccount, did: EncodeDIDKey(peerPub), binding: &binding}, signer
}

// The question laye answered is asked again when the device answers. Nothing
// changed, so the answer is the same.
func TestAHalfAdmissionStillProvesWhatItStoodOn(t *testing.T) {
	h := handlerWithCreds(t)
	half, _ := vouchedHalfAdmission(t, h)

	assert.NoError(t, h.stillProven(half))
}

// Striking the signer out of auth.binding_signers reaches a half-admission
// already opened: the binding it stood on no longer counts, whoever it names.
func TestStrikingTheSignerRevokesTheHalfAdmission(t *testing.T) {
	h := handlerWithCreds(t)
	half, _ := vouchedHalfAdmission(t, h)

	h.SetIdentities([]string{mastodonAccount}, nil)

	require.Error(t, h.stillProven(half))
}

// Striking the account does too, as it always did.
func TestStrikingTheAccountRevokesTheHalfAdmission(t *testing.T) {
	h := handlerWithCreds(t)
	half, _ := vouchedHalfAdmission(t, h)

	h.SetIdentities([]string{atprotoAccount}, h.identities.trustedSigners())

	require.Error(t, h.stillProven(half))
}

// A binding for one account cannot stand under a half-admission for another.
func TestABindingForAnotherAccountDoesNotProveThisOne(t *testing.T) {
	h := handlerWithCreds(t)
	half, _ := vouchedHalfAdmission(t, h)
	h.SetIdentities([]string{mastodonAccount, atprotoAccount}, h.identities.trustedSigners())
	half.identity = atprotoAccount

	require.Error(t, h.stillProven(half))
}

// A did:key route is its own proof: the signature was the whole of it, there
// is no binding behind it, and the list is all there is to ask.
func TestAKeyRouteIsProvenByTheList(t *testing.T) {
	h := handlerAdmitting(t, atprotoAccount)

	assert.NoError(t, h.stillProven(halfAdmission{identity: atprotoAccount}))
	h.SetIdentities(nil, nil)
	assert.Error(t, h.stillProven(halfAdmission{identity: atprotoAccount}))
}

// What admitted the half-admission rides with it to the ceremony that spends
// it, so the gate has the binding and not only the name.
func TestTheHalfAdmissionCarriesItsBindingToTheGate(t *testing.T) {
	h := handlerWithCreds(t)
	half, _ := vouchedHalfAdmission(t, h)
	token, err := h.pendingLogins.openWith(half)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/auth/login/finish", nil)
	req.AddCookie(&http.Cookie{Name: pendingCookieName, Value: token})
	p := h.presented(req)

	require.True(t, p.PendingLive)
	assert.Equal(t, mastodonAccount, p.Pending)
	require.NotNil(t, p.pending.binding)
	assert.Equal(t, half.did, p.pending.did)
	assert.NoError(t, h.stillProven(p.pending))
}
