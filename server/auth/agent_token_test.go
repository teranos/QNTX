package auth

import (
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/teranos/QNTX/internal/access"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "「[Opus 5.5] 2. fine change it, fix it」"

// 「[Opus 5.5] agentsNode is a node with a key of its own, ROOT listed, and a token ROOT」
// 「[Opus 5.5] minted at ATTESTOR in ground: the laptop's token, under which agents start.」
func agentsNode(t *testing.T) (h *Handler, store *memTokenStore, raw string, asking Grant) {
	t.Helper()
	store = newMemTokenStore()
	h = &Handler{users: &memUsers{}, sessions: newSessionStore(1), tokens: store, logger: testLogger()}
	h.SetIdentities([]string{mastodonAccount}, nil)
	h.SetNodeKey(ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize)))
	raw, _, err := store.Create(NewToken{Label: "ground-token", MintedBy: mastodonAccount, MintedByUser: "US-root",
		Level: LevelAttestor, Namespaces: []string{"ground"}, ExpiresAt: NeverEnds()})
	require.NoError(t, err)
	asking, ok := store.Lookup(sha256Hex(raw))
	require.True(t, ok)
	return h, store, raw, asking
}

// 「[Opus 5.5] presentedAs is what the gate makes of a bearer: false when it admits nobody.」
func presentedAs(h *Handler, raw string) (Admission, bool) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	return h.admissionOf(h.presented(req))
}

// agentOfToken is what an agent-of token held from before carries.
type agentOfToken struct{ Token, DID, Label string }

// agentOf is an agent-of token already held, as the node derived and wrote it
// before it stopped handing them out.
func agentOf(t *testing.T, h *Handler, store *memTokenStore, asking Grant, agent string) agentOfToken {
	t.Helper()
	label := agentOfLabel + ":" + asking.DID + ":" + agent
	raw, did, err := access.DeriveToken(h.nodeKey, label)
	require.NoError(t, err)
	_, err = store.Issue(IssuedToken{Hash: sha256Hex(raw), DID: did, Label: label,
		MintedBy: asking.MintedBy, Level: asking.Level, Namespaces: asking.Namespaces, ExpiresAt: NeverEnds()})
	require.NoError(t, err)
	return agentOfToken{Token: raw, DID: did, Label: label}
}

// "Tokens are to be removed from QNTX except for third-party OAuth clients"
func TestNoAgentIsHandedAToken(t *testing.T) {
	h, _, _, _ := agentsNode(t)
	h.corsWrap = func(next http.HandlerFunc) http.HandlerFunc { return next }
	_, served := h.Routes()["/auth/agent"]
	assert.False(t, served, "/auth/agent still hands an agent a token")
}

// 「[Opus 5.5] It holds what the asking token's label holds, and a grant naming the」
// 「[Opus 5.5] agent's own label widens nothing: it reaches no more than the token it ran under.」
func TestAnAgentsTokenHoldsWhatTheAskingTokenHolds(t *testing.T) {
	h, store, _, asking := agentsNode(t)
	otari := agentOf(t, h, store, asking, "otari")
	now := time.Now()
	h.SetRoleReader(&memRoles{lines: map[string][]RoleLine{"ground": {
		{Routes: []string{"ground-token"}, Roles: []string{"GROUND"}, Granted: true, Actor: mastodonAccount, At: now},
		{Routes: []string{otari.Label}, Roles: []string{"WIDER"}, Granted: true, Actor: mastodonAccount, At: now},
	}}})

	admitted, ok := presentedAs(h, otari.Token)
	require.True(t, ok)
	assert.Equal(t, []string{"GROUND"}, admitted.Roles())
}

// 「[Opus 5.5] It ends with the token it ran under: revoked, it is refused; turned back on,」
// 「[Opus 5.5] it works again; past its end, it is refused.」
func TestAnAgentsTokenEndsWithTheAskingToken(t *testing.T) {
	h, store, _, asking := agentsNode(t)
	otari := agentOf(t, h, store, asking, "otari")

	require.NoError(t, store.Revoke(asking.ID))
	_, ok := presentedAs(h, otari.Token)
	assert.False(t, ok, "the agent's token outlived the revoked token it ran under")

	require.NoError(t, store.Enable(asking.ID))
	_, ok = presentedAs(h, otari.Token)
	assert.True(t, ok, "turning the asking token back on did not reach its agent")

	store.expire(asking.ID, time.Now().Add(-time.Minute))
	_, ok = presentedAs(h, otari.Token)
	assert.False(t, ok, "the agent's token outlived the expired token it ran under")
}

// 「[Opus 5.5] A derived token's label is the node's to write: a session mints none.」
func TestASessionMintsNoAgentsLabel(t *testing.T) {
	h, _ := grantHandler(t)
	rec := httptest.NewRecorder()
	mint(h, rec, mintRequest(`{"expires_at":"never","label":"`+agentOfLabel+`:did:key:z6Mkx:otari","level":"SUPER"}`, ""))
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

// 「[Opus 5.5] expire sets when a token ends, as a store that read it back would hold it.」
func (m *memTokenStore) expire(id string, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, tok := range m.tokens {
		if tok.id == id {
			tok.expiresAt = &at
		}
	}
}

// 「[Opus 5.5] A label is not a derivation. A token named as an agent's that the node's key」
// 「[Opus 5.5] did not derive under that name stands on nobody's roles: it is refused.」
func TestALabelAloneMakesNoAgentsToken(t *testing.T) {
	h, store, _, asking := agentsNode(t)
	h.SetRoleReader(&memRoles{lines: map[string][]RoleLine{"ground": {
		{Routes: []string{"ground-token"}, Roles: []string{"GROUND"}, Granted: true, Actor: mastodonAccount, At: time.Now()},
	}}})
	// 「[Opus 5.5] Minted before a session was refused such a label, the way one already held is.」
	named, _, err := store.Create(NewToken{Label: agentOfLabel + ":" + asking.DID + ":otari", MintedBy: mastodonAccount,
		Level: LevelAttestor, Namespaces: []string{"ground"}, ExpiresAt: NeverEnds()})
	require.NoError(t, err)

	_, ok := presentedAs(h, named)
	assert.False(t, ok, "a token the node did not derive stood on the asking token's roles")
}

// 「[Opus 5.5] What the tokens element shows of an agent's token is what the gate admits it」
// 「[Opus 5.5] as: the asking token's namespaces and roles, and none once that token is off.」
func TestAnAgentsTokenIsShownAsTheGateAdmitsIt(t *testing.T) {
	h, store, _, asking := agentsNode(t)
	h.SetRoleReader(&memRoles{lines: map[string][]RoleLine{"ground": {
		{Routes: []string{"ground-token"}, Roles: []string{"GROUND"}, Granted: true, Actor: mastodonAccount, At: time.Now()},
	}}})
	otari := agentOf(t, h, store, asking, "otari")
	info := listed(t, store, otari.DID)

	assert.Equal(t, map[string][]string{"ground": {"GROUND"}}, h.answerFor(info).Roles)

	require.NoError(t, store.Revoke(asking.ID))
	assert.Empty(t, h.answerFor(info).Roles, "an agent whose token is off was shown holding roles")
}

// 「[Opus 5.5] listed is the token whose DID this is, as the store lists it.」
func listed(t *testing.T, store *memTokenStore, did string) TokenInfo {
	t.Helper()
	held, err := store.List()
	require.NoError(t, err)
	for _, info := range held {
		if info.DID == did {
			return info
		}
	}
	t.Fatalf("%s is not listed", did)
	return TokenInfo{}
}

// Where an agent's token is shown acting is where the token it runs under acts
// now, beside the roles it holds there; nowhere once that token is off.
func TestAnAgentsTokenIsShownWhereItActsNow(t *testing.T) {
	h, store, _, asking := agentsNode(t)
	h.SetRoleReader(&memRoles{lines: map[string][]RoleLine{"orchard": {
		{Routes: []string{"ground-token"}, Roles: []string{"PICKER"}, Granted: true, Actor: mastodonAccount, At: time.Now()},
	}}})
	otari := agentOf(t, h, store, asking, "otari")
	require.NoError(t, store.SetNamespaces(asking.ID, []string{"orchard"}))
	info := listed(t, store, otari.DID)
	require.Equal(t, []string{"ground"}, info.Namespaces, "the record keeps where it was handed")

	shown := h.answerFor(info)
	assert.Equal(t, []string{"orchard"}, shown.Namespaces)
	assert.Equal(t, map[string][]string{"orchard": {"PICKER"}}, shown.Roles)

	require.NoError(t, store.Revoke(asking.ID))
	assert.Empty(t, h.answerFor(info).Namespaces, "an agent whose token is off was shown acting somewhere")
}
