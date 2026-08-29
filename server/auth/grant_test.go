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

	req := mintRequest(`{"label":"ingest","scope":{"write":["ingested"]}}`, "")
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

	mint(h, rec, mintRequest(`{"label":"mbp"}`, ""))

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
		`{"label":"ingest","scope":{"write":["ingested"]}}`, session))
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

// A node opens one attestation store and pins it to default (ADR-026), so a
// token naming another namespace is refused on every use. Minting it is the
// reporting-success failure one step earlier.
func TestANamespaceTheNodeCannotServeIsRefusedAtMint(t *testing.T) {
	h, store := grantHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	mint(h, rec, mintRequest(
		`{"label":"other","namespace":"pond","scope":{"read":["noted"]}}`, session))

	require.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), "pond")
	assert.NotContains(t, rec.Body.String(), NamespaceDefault,
		"the refusal names what this node serves")

	listed, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, listed, "a token that could not be used was minted anyway")
}

// Naming a namespace is crossing into one. Without this any session could mint
// itself a credential for a namespace it was never admitted to.
func TestNamingANamespaceNeedsAListedIdentity(t *testing.T) {
	h, _ := grantHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)

	// A session that logged in as nobody — the ungoverned case.
	rec := httptest.NewRecorder()
	mint(h, rec, mintRequest(
		`{"label":"sneak","namespace":"did:key:zproject","scope":{"read":["noted"]}}`, ""))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	assert.Contains(t, rec.Body.String(), "no identity")
	assert.NotContains(t, rec.Body.String(), "root_identities",
		"the refusal names a config key")

	// A listed identity gets past this check and lands on the next one: the
	// node has nowhere to put a token for another namespace.
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	rec = httptest.NewRecorder()
	mint(h, rec, mintRequest(
		`{"label":"fine","namespace":"did:key:zproject","scope":{"read":["noted"]}}`, session))
	assert.Equal(t, http.StatusConflict, rec.Code)
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
		`{"label":"late","namespace":"did:key:zproject","scope":{"read":["noted"]}}`, session))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// default is where a session mints without naming anything, so an ungoverned
// deployment keeps working rather than losing tokens entirely.
func TestDefaultNamespaceNeedsNoListedIdentity(t *testing.T) {
	h, _ := grantHandler(t)
	rec := httptest.NewRecorder()

	mint(h, rec, mintRequest(
		`{"label":"ordinary","scope":{"write":["ingested"]}}`, ""))

	assert.Equal(t, http.StatusOK, rec.Code)
}

// system holds the node's key and the tokens themselves. A credential that
// could act there could rewrite what credentials are.
func TestNoTokenActsInTheSystemNamespace(t *testing.T) {
	h, _ := grantHandler(t)
	rec := httptest.NewRecorder()

	mint(h, rec, mintRequest(
		`{"label":"nope","namespace":"system","scope":{"read":["noted"]}}`, ""))

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// The middleware is where a token stops being a string and starts being a
// caller. Everything downstream reads this or the scope means nothing.
func TestTheMiddlewareHandsDownTheGrant(t *testing.T) {
	h, store := grantHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	raw, _, err := store.Create(NewToken{
		Label:      "ingest",
		MintedBy:   mastodonAccount,
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
	assert.Equal(t, LevelSuper, seen.Level)
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
