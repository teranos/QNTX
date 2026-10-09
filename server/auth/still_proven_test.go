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

// A did:key route is its own proof: the identity is the key that signed, there
// is no binding behind it, and the list is all there is to ask.
func TestAKeyRouteIsProvenByTheList(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	key := EncodeDIDKey(pub)
	h := handlerAdmitting(t, key)

	assert.NoError(t, h.stillProven(halfAdmission{identity: key, did: key}))
	h.SetIdentities(nil, nil)
	assert.Error(t, h.stillProven(halfAdmission{identity: key, did: key}))
}

// "nil is nil"

// An account is not its own proof. A half-admission for one that carries no
// binding proves nothing.
func TestAnAccountWithNoBindingIsNotProven(t *testing.T) {
	h := handlerAdmitting(t, atprotoAccount)
	pub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	require.Error(t, h.stillProven(halfAdmission{identity: atprotoAccount, did: EncodeDIDKey(pub)}))
}

// A User carries the signed binding for each account it holds (ADR-031), so
// the route joined keeps what reached it.
func TestJoiningARouteKeepsItsBinding(t *testing.T) {
	binding := mastodonBinding("@tim@mastodon.example")

	u := withRoute(User{}, mastodonAccount, binding)

	require.Len(t, u.Accounts, 1)
	assert.Equal(t, binding, u.Accounts[0].Binding)
}

// The binding an account was reached by is asked about again when a passkey
// answers for that account: signer still trusted, signature still good, and
// about a key this User holds.
func TestAHeldBindingIsAskedAboutAgain(t *testing.T) {
	h := handlerWithCreds(t)
	half, _ := vouchedHalfAdmission(t, h)
	u := User{ID: "US-TIM", Level: LevelRoot,
		Keys: []UserKey{{DID: half.did, Origin: OriginBrowser}}}
	u = withRoute(u, mastodonAccount, half.binding)

	assert.NoError(t, h.heldBindingStillCounts(u, mastodonAccount))

	// A key the User holds is its own route, and its signature was the proof.
	assert.NoError(t, h.heldBindingStillCounts(u, half.did))

	// A route that is not this User's account, an account with no binding
	// kept, and no User at all: each is nothing proven.
	require.Error(t, h.heldBindingStillCounts(u, atprotoAccount))
	require.Error(t, h.heldBindingStillCounts(User{}, mastodonAccount))
	unbound := User{ID: "US-TIM", Accounts: []UserAccount{{Provider: "mastodon", CanonicalID: mastodonAccount}}}
	require.Error(t, h.heldBindingStillCounts(unbound, mastodonAccount))

	// The signer is struck out: the binding written down no longer counts.
	h.SetIdentities([]string{mastodonAccount}, nil)
	require.Error(t, h.heldBindingStillCounts(u, mastodonAccount))
}

// An account joined before bindings were kept gets the binding its provider
// sign-in just proved, so the passkey after it has one to ask about.
func TestASignInKeepsTheBindingAnAccountLacked(t *testing.T) {
	h := handlerWithCreds(t)
	half, _ := vouchedHalfAdmission(t, h)
	store := &memUsers{}
	h.users = store
	require.NoError(t, store.Put(User{ID: "US-TIM", Level: LevelRoot,
		Accounts: []UserAccount{{Provider: "mastodon", CanonicalID: mastodonAccount}}}))

	u, err := h.joinUser(mastodonAccount, half.binding, half.did)
	require.NoError(t, err)

	require.Len(t, u.Accounts, 1)
	assert.Equal(t, half.binding, u.Accounts[0].Binding)
	kept, _, err := store.ByRoute(mastodonAccount)
	require.NoError(t, err)
	assert.Equal(t, half.binding, kept.Accounts[0].Binding, "the binding was not written down")
	assert.NoError(t, h.heldBindingStillCounts(kept, mastodonAccount))
}

// A binding about a key the User does not hold reaches nobody, whoever
// signed it.
func TestAHeldBindingAboutAnotherKeyDoesNotCount(t *testing.T) {
	h := handlerWithCreds(t)
	half, _ := vouchedHalfAdmission(t, h)
	u := withRoute(User{ID: "US-TIM", Level: LevelRoot}, mastodonAccount, half.binding)

	require.Error(t, h.heldBindingStillCounts(u, mastodonAccount))
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
