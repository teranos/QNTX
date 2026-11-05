package auth

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "2. fine change it, fix it"

// agentsNode is a node with a key of its own, ROOT listed, and a token ROOT
// minted at ATTESTOR in ground: the laptop's token, under which agents start.
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

// presentedAs is what the gate makes of a bearer: false when it admits nobody.
func presentedAs(h *Handler, raw string) (Admission, bool) {
	req := httptest.NewRequest(http.MethodPost, agentTokenPath, nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	return h.admissionOf(h.presented(req))
}

// asksForAgent is POST /auth/agent as admitted, the way Middleware hands it on.
func asksForAgent(h *Handler, admitted Admission, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, agentTokenPath, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.handleAgentToken(rec, req.WithContext(WithAdmission(context.Background(), admitted)))
	return rec
}

// agentHanded is what the node answered an agent with.
type agentHanded struct {
	Token     string `json:"token"`
	DID       string `json:"did"`
	Label     string `json:"label"`
	Agent     string `json:"agent"`
	DerivedBy string `json:"derived_by"`
}

func handedTo(t *testing.T, h *Handler, raw, agent string) agentHanded {
	t.Helper()
	admitted, ok := presentedAs(h, raw)
	require.True(t, ok, "the asking token was not admitted")
	rec := asksForAgent(h, admitted, `{"agent":"`+agent+`"}`)
	require.Contains(t, []int{http.StatusCreated, http.StatusOK}, rec.Code, rec.Body.String())
	var handed agentHanded
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &handed))
	return handed
}

// Each agent started under one token is its own DID, and the same agent
// asking again is the same token: nothing is kept for it but the token record.
func TestEachAgentATokenStartsHoldsATokenOfItsOwn(t *testing.T) {
	h, store, raw, asking := agentsNode(t)

	otari := handedTo(t, h, raw, "otari")
	assert.NotEqual(t, asking.DID, otari.DID, "the agent asks as the token it ran under")
	assert.Equal(t, asking.DID, otari.DerivedBy)
	assert.Equal(t, "otari", otari.Agent)

	again := handedTo(t, h, raw, "otari")
	assert.Equal(t, otari.Token, again.Token, "the same agent was handed another token")
	assert.Equal(t, otari.DID, again.DID)

	sion := handedTo(t, h, raw, "6sion")
	assert.NotEqual(t, otari.DID, sion.DID, "two agents are one DID")

	held, err := store.List()
	require.NoError(t, err)
	assert.Len(t, held, 3, "the asking token and one per agent")

	admitted, ok := presentedAs(h, otari.Token)
	require.True(t, ok, "the agent's token admits nobody")
	assert.Equal(t, otari.DID, admitted.ActsAs(), "the agent attests as itself")
	assert.Equal(t, mastodonAccount, admitted.Identity, "it speaks for whoever minted the asking token")
	assert.Equal(t, string(LevelAttestor), admitted.LevelName())
	assert.Equal(t, []string{"ground"}, admitted.Namespaces)
	assert.Empty(t, admitted.ClientDID, "i name refuses a client's token, and this is none")
}

// It holds what the asking token's label holds, and a grant naming the
// agent's own label widens nothing: it reaches no more than the token it ran under.
func TestAnAgentsTokenHoldsWhatTheAskingTokenHolds(t *testing.T) {
	h, _, raw, _ := agentsNode(t)
	otari := handedTo(t, h, raw, "otari")
	now := time.Now()
	h.SetRoleReader(&memRoles{lines: map[string][]RoleLine{"ground": {
		{Routes: []string{"ground-token"}, Roles: []string{"GROUND"}, Granted: true, Actor: mastodonAccount, At: now},
		{Routes: []string{otari.Label}, Roles: []string{"WIDER"}, Granted: true, Actor: mastodonAccount, At: now},
	}}})

	admitted, ok := presentedAs(h, otari.Token)
	require.True(t, ok)
	assert.Equal(t, []string{"GROUND"}, admitted.Roles())
}

// It ends with the token it ran under: revoked, it is refused; turned back on,
// it works again; past its end, it is refused.
func TestAnAgentsTokenEndsWithTheAskingToken(t *testing.T) {
	h, store, raw, asking := agentsNode(t)
	otari := handedTo(t, h, raw, "otari")

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

// Its end is the asking token's end, on the record it is listed by.
func TestAnAgentsTokenEndsWhenTheAskingTokenDoes(t *testing.T) {
	h, store, _, _ := agentsNode(t)
	ends := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	raw, _, err := store.Create(NewToken{Label: "for-an-hour", MintedBy: mastodonAccount,
		Level: LevelToken, Namespaces: []string{"ground"}, ExpiresAt: &ends})
	require.NoError(t, err)

	otari := handedTo(t, h, raw, "otari")
	held, err := store.List()
	require.NoError(t, err)
	for _, info := range held {
		if info.DID == otari.DID {
			require.NotNil(t, info.ExpiresAt)
			at, err := time.Parse(time.RFC3339Nano, *info.ExpiresAt)
			require.NoError(t, err)
			assert.True(t, at.Equal(ends), "it ends at %s, and the asking token at %s", at, ends)
			assert.Equal(t, LevelToken, info.Level)
			return
		}
	}
	t.Fatal("the agent's token is not listed")
}

// Who is handed nothing: ROOT's kind, SUPER, a token issued through a client,
// an agent's own token, and a session.
func TestWhoIsHandedNoAgentToken(t *testing.T) {
	h, store, raw, _ := agentsNode(t)

	rootRaw, rootDID := nodeAgentToken(t)
	require.NoError(t, h.HoldNodeAgent(rootRaw, rootDID))
	superRaw, _, err := store.Create(NewToken{Label: "mbp", MintedBy: mastodonAccount, Level: LevelSuper, ExpiresAt: NeverEnds()})
	require.NoError(t, err)
	clientsRaw := "qntx_" + strings.Repeat("c", 64)
	_, err = store.Issue(IssuedToken{Hash: sha256Hex(clientsRaw), DID: "did:key:z6Mkissued", Label: "app",
		MintedBy: mastodonAccount, Level: LevelToken, Namespaces: []string{"ground"},
		ClientDID: "did:key:z6Mkclient", ExpiresAt: NeverEnds()})
	require.NoError(t, err)
	otari := handedTo(t, h, raw, "otari")

	before, err := store.List()
	require.NoError(t, err)
	for asked, token := range map[string]string{"ROOT's kind": rootRaw, "SUPER": superRaw, "a client's token": clientsRaw, "an agent's token": otari.Token} {
		admitted, ok := presentedAs(h, token)
		require.True(t, ok, asked)
		rec := asksForAgent(h, admitted, `{"agent":"hazel"}`)
		assert.Equal(t, http.StatusForbidden, rec.Code, "%s: %s", asked, rec.Body.String())
	}
	session := Admitted(LevelRoot)
	session.Identity = mastodonAccount
	rec := asksForAgent(h, session, `{"agent":"hazel"}`)
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	after, err := store.List()
	require.NoError(t, err)
	assert.Len(t, after, len(before), "a token was handed to who is handed none")
}

// An agent is named by one line of text.
func TestAnAgentNamedByNoLineIsRefused(t *testing.T) {
	h, store, raw, _ := agentsNode(t)
	admitted, ok := presentedAs(h, raw)
	require.True(t, ok)
	for _, body := range []string{`{}`, `{"agent":"  "}`, `{"agent":"two\nlines"}`, `{"agent":"` + strings.Repeat("n", MaxName+1) + `"}`, `not json`} {
		rec := asksForAgent(h, admitted, body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, body)
	}
	held, err := store.List()
	require.NoError(t, err)
	assert.Len(t, held, 1)
}

// The record says which token it was derived for, by whom, and for which agent.
func TestAnAgentsTokenIsAttestedWithWhoDerivedIt(t *testing.T) {
	h, _, raw, asking := agentsNode(t)
	kept := &memAttestor{}
	h.SetAttestor(kept)

	otari := handedTo(t, h, raw, "otari")
	handedTo(t, h, raw, "otari")

	require.Len(t, kept.wrote, 1, "asking again wrote the token down again")
	line := kept.wrote[0]
	assert.Equal(t, []string{PredicateMinted}, line.Predicates)
	assert.Equal(t, []string{mastodonAccount}, line.Subjects)
	assert.Equal(t, asking.DID, line.Attributes["derived_by"])
	assert.Equal(t, "otari", line.Attributes["agent"])
	assert.Equal(t, otari.DID, line.Attributes["did"])
}

// A derived token's label is the node's to write: a session mints none.
func TestASessionMintsNoAgentsLabel(t *testing.T) {
	h, _ := grantHandler(t)
	rec := httptest.NewRecorder()
	mint(h, rec, mintRequest(`{"expires_at":"never","label":"`+agentOfLabel+`:did:key:z6Mkx:otari","level":"SUPER"}`, ""))
	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

// expire sets when a token ends, as a store that read it back would hold it.
func (m *memTokenStore) expire(id string, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, tok := range m.tokens {
		if tok.id == id {
			tok.expiresAt = &at
		}
	}
}

// A label is not a derivation. A token named as an agent's that the node's key
// did not derive under that name stands on nobody's roles: it is refused.
func TestALabelAloneMakesNoAgentsToken(t *testing.T) {
	h, store, _, asking := agentsNode(t)
	h.SetRoleReader(&memRoles{lines: map[string][]RoleLine{"ground": {
		{Routes: []string{"ground-token"}, Roles: []string{"GROUND"}, Granted: true, Actor: mastodonAccount, At: time.Now()},
	}}})
	// Minted before a session was refused such a label, the way one already held is.
	named, _, err := store.Create(NewToken{Label: agentLabel(asking.DID, "otari"), MintedBy: mastodonAccount,
		Level: LevelAttestor, Namespaces: []string{"ground"}, ExpiresAt: NeverEnds()})
	require.NoError(t, err)

	_, ok := presentedAs(h, named)
	assert.False(t, ok, "a token the node did not derive stood on the asking token's roles")
}

// Agents asking at once are handed one token, and it is written down once:
// two asks that both found it absent would both write it, and the second write
// would turn back on a token switched off in between.
func TestAgentsAskingAtOnceAreHandedOneToken(t *testing.T) {
	h, store, raw, _ := agentsNode(t)
	kept := &memAttestor{}
	h.SetAttestor(kept)
	admitted, ok := presentedAs(h, raw)
	require.True(t, ok)
	// Each look holds until the other has looked too, or until it is plain
	// the other cannot look while this one asks.
	both := &lookingTogether{memTokenStore: store, looked: make(chan struct{}, 2)}
	h.tokens = both

	codes := make(chan int, 2)
	for range 2 {
		go func() { codes <- asksForAgent(h, admitted, `{"agent":"otari"}`).Code }()
	}
	created := 0
	for range 2 {
		if <-codes == http.StatusCreated {
			created++
		}
	}
	assert.Equal(t, 1, created, "both asks found the token absent and wrote it")
	assert.Len(t, kept.wrote, 1, "the token was attested as minted more than once")
}

// lookingTogether is a store whose LookupSpent waits for a second look, so two
// asks that may look at once do.
type lookingTogether struct {
	*memTokenStore
	looked chan struct{}
}

func (l *lookingTogether) LookupSpent(hash string) (Grant, bool, bool) {
	grant, live, held := l.memTokenStore.LookupSpent(hash)
	l.looked <- struct{}{}
	deadline := time.After(200 * time.Millisecond)
	for len(l.looked) < 2 {
		select {
		case <-deadline:
			return grant, live, held
		case <-time.After(time.Millisecond):
		}
	}
	return grant, live, held
}

// What the tokens element shows of an agent's token is what the gate admits it
// as: the asking token's namespaces and roles, and none once that token is off.
func TestAnAgentsTokenIsShownAsTheGateAdmitsIt(t *testing.T) {
	h, store, raw, asking := agentsNode(t)
	h.SetRoleReader(&memRoles{lines: map[string][]RoleLine{"ground": {
		{Routes: []string{"ground-token"}, Roles: []string{"GROUND"}, Granted: true, Actor: mastodonAccount, At: time.Now()},
	}}})
	otari := handedTo(t, h, raw, "otari")
	info := listed(t, store, otari.DID)

	assert.Equal(t, map[string][]string{"ground": {"GROUND"}}, h.answerFor(info).Roles)

	require.NoError(t, store.Revoke(asking.ID))
	assert.Empty(t, h.answerFor(info).Roles, "an agent whose token is off was shown holding roles")
}

// listed is the token whose DID this is, as the store lists it.
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
	h, store, raw, asking := agentsNode(t)
	h.SetRoleReader(&memRoles{lines: map[string][]RoleLine{"orchard": {
		{Routes: []string{"ground-token"}, Roles: []string{"PICKER"}, Granted: true, Actor: mastodonAccount, At: time.Now()},
	}}})
	otari := handedTo(t, h, raw, "otari")
	require.NoError(t, store.SetNamespaces(asking.ID, []string{"orchard"}))
	info := listed(t, store, otari.DID)
	require.Equal(t, []string{"ground"}, info.Namespaces, "the record keeps where it was handed")

	shown := h.answerFor(info)
	assert.Equal(t, []string{"orchard"}, shown.Namespaces)
	assert.Equal(t, map[string][]string{"orchard": {"PICKER"}}, shown.Roles)

	require.NoError(t, store.Revoke(asking.ID))
	assert.Empty(t, h.answerFor(info).Namespaces, "an agent whose token is off was shown acting somewhere")
}
