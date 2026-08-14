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

// A token with neither scope can do nothing, so issuing one is a mistake worth
// catching at the point it is made rather than the first time it is used.
func TestAScopelessTokenIsRefused(t *testing.T) {
	h, _ := grantHandler(t)
	rec := httptest.NewRecorder()

	h.handleCreateToken(rec, mintRequest(`{"label":"useless"}`, ""))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "predicate")
}

// The session that asked is who the token speaks for. Without this a token
// traces to a label a human typed and no further.
func TestATokenRemembersWhoMintedIt(t *testing.T) {
	h, store := grantHandler(t)
	session, err := h.sessions.create(mastodonAccount)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	h.handleCreateToken(rec, mintRequest(
		`{"label":"ingest","scope":{"write":["ingested"]}}`, session))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	grant, live := store.Lookup(sha256Hex(resp.Token))
	require.True(t, live)
	assert.Equal(t, mastodonAccount, grant.MintedBy)
	assert.Equal(t, NamespaceDefault, grant.Namespace)
	assert.Equal(t, []string{"ingested"}, grant.ScopeWrite)
}

// A token acts somewhere. Naming it at mint time is what makes one deployment
// able to issue credentials for more than the default namespace.
func TestAMintedTokenTakesTheNamespaceItWasGiven(t *testing.T) {
	h, store := grantHandler(t)
	session, err := h.sessions.create(mastodonAccount)
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	h.handleCreateToken(rec, mintRequest(
		`{"label":"other","namespace":"did:key:zproject","scope":{"read":["noted"]}}`, session))
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Token     string `json:"token"`
		Namespace string `json:"namespace"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "did:key:zproject", resp.Namespace)

	grant, live := store.Lookup(sha256Hex(resp.Token))
	require.True(t, live)
	assert.Equal(t, "did:key:zproject", grant.Namespace)
}

// system holds the node's key and the tokens themselves. A credential that
// could act there could rewrite what credentials are.
func TestNoTokenActsInTheSystemNamespace(t *testing.T) {
	h, _ := grantHandler(t)
	rec := httptest.NewRecorder()

	h.handleCreateToken(rec, mintRequest(
		`{"label":"nope","namespace":"system","scope":{"read":["noted"]}}`, ""))

	assert.Equal(t, http.StatusForbidden, rec.Code)
}

// The middleware is where a token stops being a string and starts being a
// caller. Everything downstream reads this or the scope means nothing.
func TestTheMiddlewareHandsDownTheGrant(t *testing.T) {
	h, store := grantHandler(t)
	raw, _, err := store.Create(NewToken{
		Label:      "ingest",
		MintedBy:   mastodonAccount,
		Namespace:  "did:key:zproject",
		ScopeRead:  []string{"noted"},
		ScopeWrite: []string{"ingested"},
	})
	require.NoError(t, err)

	var seen Caller
	handler := h.Middleware(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = CallerFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, LevelToken, seen.Level)
	assert.Equal(t, "did:key:zproject", seen.Namespace)
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
	session, err := h.sessions.create(mastodonAccount)
	require.NoError(t, err)

	var seen Caller
	handler := h.Middleware(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = CallerFrom(r.Context())
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
