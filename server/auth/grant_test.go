package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func grantHandler(t *testing.T) (*Handler, *memTokenStore) {
	t.Helper()
	store := newMemTokenStore()
	return &Handler{
		tokens:   store,
		sessions: newSessionStore(24),
		logger:   testLogger(),
	}, store
}

func mintRequest(body, session string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/auth/tokens", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if session != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	}
	return req
}

// mint and byID call the handlers the way sessionOnly does: the gate resolves
// what the request carries, and hands the handler that.
func mint(h *Handler, rec *httptest.ResponseRecorder, req *http.Request) {
	h.handleCreateToken(rec, req, h.presented(req))
}

func byID(h *Handler, rec *httptest.ResponseRecorder, req *http.Request) {
	h.handleTokenByID(rec, req, h.presented(req))
}

// A token outlives the session that minted it, so a half-admission — which has
// no device behind it and buys one ceremony — must never be what names one.
func TestAHalfAdmissionDoesNotNameAMint(t *testing.T) {
	h, store := grantHandler(t)
	ticket, err := h.pendingLogins.open(mastodonAccount)
	require.NoError(t, err)

	req := mintRequest(`{"label":"ingest","level":"ATTESTOR","scope":{"write":["ingested"]}}`, "")
	req.AddCookie(&http.Cookie{Name: pendingCookieName, Value: ticket})
	rec := httptest.NewRecorder()
	mint(h, rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	grant, live := store.Lookup(sha256Hex(resp.Token))
	require.True(t, live)
	assert.Empty(t, grant.MintedBy)
}

// A label is the whole of what minting asks for.
func TestALabelIsAllTheMintAsksFor(t *testing.T) {
	h, _ := grantHandler(t)
	rec := httptest.NewRecorder()

	mint(h, rec, mintRequest(`{"label":"mbp","level":"ATTESTOR"}`, ""))

	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// The session that asked is who the token speaks for. Without this a token
// traces to a label a human typed and no further.
func TestATokenRemembersWhoMintedIt(t *testing.T) {
	h, store := grantHandler(t)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	mint(h, rec, mintRequest(
		`{"label":"ingest","level":"ATTESTOR","scope":{"write":["ingested"]}}`, session))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	grant, live := store.Lookup(sha256Hex(resp.Token))
	require.True(t, live)
	assert.Equal(t, mastodonAccount, grant.MintedBy)
	assert.Equal(t, []string{NamespaceDefault}, grant.Namespaces)
	assert.Equal(t, []string{"ingested"}, grant.ScopeWrite)
}

// The node opens a namespace on the first request that names it, so a token
// for one other than default is a token it can serve.
func TestANamespaceOtherThanDefaultIsMinted(t *testing.T) {
	h, store := grantHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	mint(h, rec, mintRequest(
		`{"label":"other","level":"ATTESTOR","namespace":"pond","scope":{"read":["noted"]}}`, session))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	listed, err := store.List()
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, []string{"pond"}, listed[0].Namespaces)
}

// Naming a namespace is crossing into one. Without this any session could mint
// itself a credential for a namespace it was never admitted to.
func TestNamingANamespaceNeedsAListedIdentity(t *testing.T) {
	h, _ := grantHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)

	// A session that logged in as nobody — the ungoverned case.
	rec := httptest.NewRecorder()
	mint(h, rec, mintRequest(
		`{"label":"sneak","level":"ATTESTOR","namespace":"did:key:zproject","scope":{"read":["noted"]}}`, ""))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "no identity")
	assert.NotContains(t, rec.Body.String(), "root_identities",
		"the refusal names a config key")

	// A listed identity is admitted to name one, and gets the token.
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	rec = httptest.NewRecorder()
	mint(h, rec, mintRequest(
		`{"label":"fine","level":"ATTESTOR","namespace":"did:key:zproject","scope":{"read":["noted"]}}`, session))
	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// An identity struck out of am.toml stops being able to name namespaces at the
// same moment it stops being able to log in.
func TestStrikingAnIdentityStopsItNamingNamespaces(t *testing.T) {
	h, _ := grantHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	h.SetIdentities(nil, nil)

	rec := httptest.NewRecorder()
	mint(h, rec, mintRequest(
		`{"label":"late","level":"ATTESTOR","namespace":"did:key:zproject","scope":{"read":["noted"]}}`, session))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// default is where a session mints without naming anything, so an ungoverned
// deployment keeps working rather than losing tokens entirely.
func TestDefaultNamespaceNeedsNoListedIdentity(t *testing.T) {
	h, _ := grantHandler(t)
	rec := httptest.NewRecorder()

	mint(h, rec, mintRequest(
		`{"label":"ordinary","level":"ATTESTOR","scope":{"write":["ingested"]}}`, ""))

	assert.Equal(t, http.StatusOK, rec.Code)
}

// Minting names the kind. A body that names one gets a token of that kind, and
// a body that names none gets an answer saying which two there are.
func TestMintingNamesTheKind(t *testing.T) {
	h, _ := grantHandler(t)
	rec := httptest.NewRecorder()

	mint(h, rec, mintRequest(`{"label":"unsaid"}`, ""))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), string(LevelSuper))
	assert.Contains(t, rec.Body.String(), string(LevelAttestor))
}

// The two kinds are what minting offers, and a token comes back as the kind it
// was minted as rather than as whatever the middleware decides for all of them.
func TestBothKindsAreMintedAsThemselves(t *testing.T) {
	for _, kind := range []Level{LevelSuper, LevelAttestor} {
		h, store := grantHandler(t)
		rec := httptest.NewRecorder()

		mint(h, rec, mintRequest(`{"label":"mine","level":"`+string(kind)+`"}`, ""))
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

		var resp struct {
			Token string `json:"token"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		grant, live := store.Lookup(sha256Hex(resp.Token))
		require.True(t, live)
		assert.Equal(t, kind, grant.Level)
	}
}

// The middleware is where a token stops being a string and starts being a
// caller. Everything downstream reads this or the scope means nothing.
func TestTheMiddlewareHandsDownTheGrant(t *testing.T) {
	h, store := grantHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	raw, _, err := store.Create(NewToken{
		Label:      "ingest",
		MintedBy:   mastodonAccount,
		Level:      LevelAttestor,
		Namespaces: []string{"did:key:zproject"},
		ScopeRead:  []string{"noted"},
		ScopeWrite: []string{"ingested"},
	})
	require.NoError(t, err)

	var seen Admission
	handler := h.Middleware(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = AdmissionFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, LevelAttestor, seen.Level)
	assert.Equal(t, []string{"did:key:zproject"}, seen.Namespaces)
	assert.Equal(t, mastodonAccount, seen.Identity)
	require.NotNil(t, seen.Grant)
	assert.True(t, seen.MayWrite("ingested"))
	assert.False(t, seen.MayWrite("noted"))
	assert.True(t, seen.MayRead("noted"))
	assert.False(t, seen.MayRead("ingested"))
}

// A session is not a token and carries no grant. Narrowing has to be something
// a token does rather than something everyone suffers.
func TestASessionCallerIsUnrestricted(t *testing.T) {
	h, _ := grantHandler(t)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	var seen Admission
	handler := h.Middleware(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = AdmissionFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	handler.ServeHTTP(httptest.NewRecorder(), req)

	assert.Nil(t, seen.Grant)
	assert.True(t, seen.MayRead("anything"))
	assert.True(t, seen.MayWrite("anything"))
}

// Read and write are separate answers, or a token that may report a result can
// also manufacture one.
func TestReadAndWriteScopesDoNotBorrowFromEachOther(t *testing.T) {
	grant := Grant{ScopeRead: []string{"noted"}, ScopeWrite: []string{"ingested"}}

	assert.True(t, grant.MayRead("noted"))
	assert.False(t, grant.MayWrite("noted"))
	assert.True(t, grant.MayWrite("ingested"))
	assert.False(t, grant.MayRead("ingested"))
}

// An empty scope is no permission. Reading it as "unset, therefore everything"
// would turn a partly-written record into an unrestricted credential.
func TestAnEmptyScopeGrantsNothing(t *testing.T) {
	grant := Grant{}

	assert.False(t, grant.MayRead("noted"))
	assert.False(t, grant.MayWrite("noted"))
}
