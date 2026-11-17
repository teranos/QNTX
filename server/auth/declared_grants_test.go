package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "「[Opus 5.5] The node would hold these as live grants while they're listed.」"
// "Their rows land in fleetingmem, signed by their own key."

// 「[Opus 5.5] guest is a key that never left its holder, and its did:key.」
func guest(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return key, EncodeDIDKey(key.Public().(ed25519.PublicKey))
}

// 「[Opus 5.5] declaringNode is a node whose am.toml lists guest1 acting in fleetingmem.」
func declaringNode(t *testing.T) (h *Handler, store *memTokenStore, key ed25519.PrivateKey, did string) {
	t.Helper()
	store = newMemTokenStore()
	h = &Handler{users: &memUsers{}, sessions: newSessionStore(1), tokens: store, logger: testLogger()}
	h.SetIdentities([]string{mastodonAccount}, nil)
	h.configuredOrigin = signedNodeOrigin
	key, did = guest(t)
	require.NoError(t, h.SetGrants([]DeclaredGrant{{Label: "guest1", DID: did, Namespaces: []string{"fleetingmem"}}}))
	return h, store, key, did
}

// 「[Opus 5.5] A key auth.grants lists is admitted on its signature: ATTESTOR in the」
// 「[Opus 5.5] namespaces its line names, acting as itself, so what it writes is signed by」
// 「[Opus 5.5] its own key, and speaking for whoever am.toml is ROOT's.」
func TestAKeyAmTomlListsIsAdmittedOnItsSignature(t *testing.T) {
	h, _, key, did := declaringNode(t)
	admitted, ok := presentedSigned(h, signature(t, key, did, signedNodeOrigin, time.Minute))
	require.True(t, ok, "a signature by a key auth.grants lists was not admitted")
	assert.Equal(t, did, admitted.ActsAs())
	assert.Equal(t, mastodonAccount, admitted.Identity)
	assert.Equal(t, string(LevelAttestor), admitted.LevelName())
	assert.Equal(t, []string{"fleetingmem"}, admitted.Namespaces)
	assert.True(t, admitted.MayActIn("fleetingmem"))
	assert.False(t, admitted.MayActIn("default"))
	require.NotNil(t, admitted.Grant)
	assert.Equal(t, "guest1", admitted.Grant.Label)
}

// 「[Opus 5.5] A line struck from auth.grants ends that key's grant: the next signature」
// 「[Opus 5.5] is refused, and the keys still listed are admitted as before.」
func TestALineStruckFromAmTomlEndsItsGrant(t *testing.T) {
	h, _, key, did := declaringNode(t)
	otherKey, otherDID := guest(t)
	require.NoError(t, h.SetGrants([]DeclaredGrant{{Label: "guest2", DID: otherDID, Namespaces: []string{"fleetingmem"}}}))

	_, ok := presentedSigned(h, signature(t, key, did, signedNodeOrigin, time.Minute))
	assert.False(t, ok, "a key struck from auth.grants was admitted")
	_, ok = presentedSigned(h, signature(t, otherKey, otherDID, signedNodeOrigin, time.Minute))
	assert.True(t, ok, "a key still listed was refused")
}

// 「[Opus 5.5] A grant held for a signature names nothing a bearer can present: what a」
// 「[Opus 5.5] listed key is admitted on is its signature, and nothing reusable.」
func TestAKeyAmTomlListsIsAdmittedOnlyOnASignature(t *testing.T) {
	h, _, _, did := declaringNode(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+did)
	_, ok := h.admissionOf(h.presented(req))
	assert.False(t, ok)
}

// 「[Opus 5.5] A list that does not hold is refused whole, naming why, and the grants」
// 「[Opus 5.5] held before stand.」
func TestAnAuthGrantsListThatDoesNotHoldLeavesTheGrantsBefore(t *testing.T) {
	h, store, key, did := declaringNode(t)
	_, otherDID := guest(t)
	_, err := store.Issue(IssuedToken{Hash: signedOnly(otherDID), DID: otherDID, Label: "laptop", MintedBy: mastodonAccount,
		Level: LevelAttestor, Namespaces: []string{"ground"}, ExpiresAt: NeverEnds()})
	require.NoError(t, err)

	for name, listed := range map[string][]DeclaredGrant{
		"a label twice": {
			{Label: "guest1", DID: did, Namespaces: []string{"fleetingmem"}},
			{Label: "guest1", DID: "did:key:z6MkhaXgBZDvotDkL5257faiztiGiC2QtKLGpbnnEGta2doK", Namespaces: []string{"fleetingmem"}},
		},
		"a key twice": {
			{Label: "guest1", DID: did, Namespaces: []string{"fleetingmem"}},
			{Label: "guest2", DID: did, Namespaces: []string{"fleetingmem"}},
		},
		"no namespace":           {{Label: "guest1", DID: did}},
		"a namespace twice":      {{Label: "guest1", DID: did, Namespaces: []string{"fleetingmem", "FleetingMem"}}},
		"no did:key":             {{Label: "guest1", DID: "guest1", Namespaces: []string{"fleetingmem"}}},
		"no label":               {{Label: " ", DID: did, Namespaces: []string{"fleetingmem"}}},
		"a minted token's label": {{Label: "laptop", DID: did, Namespaces: []string{"fleetingmem"}}},
		"a minted token's key":   {{Label: "guest1", DID: otherDID, Namespaces: []string{"fleetingmem"}}},
	} {
		assert.Error(t, h.SetGrants(listed), name)
		_, ok := presentedSigned(h, signature(t, key, did, signedNodeOrigin, time.Minute))
		assert.True(t, ok, name+": the grant held before did not stand")
	}
}

// 「[Opus 5.5] A label auth.grants lists is no label a token can be minted under: the」
// 「[Opus 5.5] roles a label holds are granted to it, and two holders would share them.」
func TestALabelAmTomlListsIsMintedUnderByNobody(t *testing.T) {
	h, _, _, _ := declaringNode(t)
	rec := httptest.NewRecorder()
	mint(h, rec, mintRequest(`{"expires_at":"never","label":"guest1","level":"ATTESTOR","namespaces":["fleetingmem"]}`, ""))
	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
}

// 「[Opus 5.5] A guest speaks for whoever am.toml is ROOT's: struck out, its guests are」
// 「[Opus 5.5] refused with it.」
func TestAGuestIsRefusedOnceRootIsStruckOut(t *testing.T) {
	h, _, key, did := declaringNode(t)
	h.SetIdentities(nil, nil)
	_, ok := presentedSigned(h, signature(t, key, did, signedNodeOrigin, time.Minute))
	assert.False(t, ok, "a guest was admitted with nobody left in auth.root_identities")
}

// 「[Opus 5.5] A list of grants with nobody in auth.root_identities is refused: there is」
// 「[Opus 5.5] nobody for them to speak for.」
func TestAuthGrantsWithNoRootIsRefused(t *testing.T) {
	h := &Handler{users: &memUsers{}, sessions: newSessionStore(1), tokens: newMemTokenStore(), logger: testLogger()}
	_, did := guest(t)
	assert.Error(t, h.SetGrants([]DeclaredGrant{{Label: "guest1", DID: did, Namespaces: []string{"fleetingmem"}}}))
}
