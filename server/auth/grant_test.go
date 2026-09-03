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

// A SUPER token is not narrowed, so a label and the kind are the whole of it.
func TestALabelIsAllASuperMintAsksFor(t *testing.T) {
	h, _ := grantHandler(t)
	rec := httptest.NewRecorder()

	mint(h, rec, mintRequest(`{"label":"mbp","level":"SUPER"}`, ""))

	assert.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// An ATTESTOR attests the way it was set up to, so minting one asks what that
// is. A token that may touch nothing is a credential with no use for anybody.
func TestAnAttestorNamesWhatItMayAttest(t *testing.T) {
	h, store := grantHandler(t)
	rec := httptest.NewRecorder()

	mint(h, rec, mintRequest(`{"label":"useless","level":"ATTESTOR"}`, ""))

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	listed, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, listed, "a token that could touch nothing was minted anyway")
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
		`{"label":"other","level":"ATTESTOR","namespaces":["pond"],"scope":{"read":["noted"]}}`, session))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	listed, err := store.List()
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Equal(t, []string{"pond"}, listed[0].Namespaces)
}

// A SUPER token is not scoped, so a scope is not what says how far it reaches.
// Reading its empty scope as a scope makes the kind that does pretty much
// everything do almost none of it, and every read through it answer nothing.
func TestASuperTokenIsNotScoped(t *testing.T) {
	super := Grant{Level: LevelSuper}

	assert.True(t, super.MayRead("anything"))
	assert.True(t, super.MayWrite("anything"))
	assert.True(t, super.Unrestricted(), "a query through it goes out as it came in")
}

// An ATTESTOR is scoped, and an empty one reaches nothing rather than
// everything — what it may attest is the whole of what it is for.
func TestAnAttestorReachesItsScopeAndNoFurther(t *testing.T) {
	attestor := Grant{Level: LevelAttestor, ScopeWrite: []string{"tpred"}}

	assert.True(t, attestor.MayWrite("tpred"))
	assert.False(t, attestor.MayWrite("something-else"))
	assert.False(t, attestor.MayRead("tpred"), "its read scope is empty")
	assert.False(t, attestor.Unrestricted())
}

// Naming a namespace is crossing into one. Without this any session could mint
// itself a credential for a namespace it was never admitted to.
func TestNamingANamespaceNeedsAListedIdentity(t *testing.T) {
	h, _ := grantHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)

	// A session that logged in as nobody — the ungoverned case.
	rec := httptest.NewRecorder()
	mint(h, rec, mintRequest(
		`{"label":"sneak","level":"ATTESTOR","namespaces":["did:key:zproject"],"scope":{"read":["noted"]}}`, ""))
	assert.Equal(t, http.StatusForbidden, rec.Code)
	// A refused caller gets the outcome. Who they are, whether that name is
	// known, and what would have let them through are the node's to keep.
	assert.Equal(t, `{"error":"refused"}`, strings.TrimSpace(rec.Body.String()))

	// A listed identity is admitted to name one, and gets the token.
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	rec = httptest.NewRecorder()
	mint(h, rec, mintRequest(
		`{"label":"fine","level":"ATTESTOR","namespaces":["did:key:zproject"],"scope":{"read":["noted"]}}`, session))
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
		`{"label":"late","level":"ATTESTOR","namespaces":["did:key:zproject"],"scope":{"read":["noted"]}}`, session))
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
	// An ATTESTOR names what it may attest, and a SUPER token is not narrowed.
	for kind, body := range map[Level]string{
		LevelSuper:    `{"label":"mine","level":"SUPER"}`,
		LevelAttestor: `{"label":"theirs","level":"ATTESTOR","scope":{"write":["ingested"]}}`,
	} {
		h, store := grantHandler(t)
		rec := httptest.NewRecorder()

		mint(h, rec, mintRequest(body, ""))
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
	handler := h.Middleware(everyLevel, func(w http.ResponseWriter, r *http.Request) {
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
	handler := h.Middleware(everyLevel, func(w http.ResponseWriter, r *http.Request) {
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
