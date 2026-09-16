package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	qntxtest "github.com/teranos/QNTX/internal/testing"

	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func testLogger() *zap.SugaredLogger {
	return zap.NewNop().Sugar()
}

// --- Session store ---

func TestSessionCreateValidate(t *testing.T) {
	store := newSessionStore(1) // 1 hour
	token, err := store.create("", User{})
	require.NoError(t, err)
	assert.Len(t, token, 64) // 32 bytes hex
	assert.True(t, store.validate(token))
}

func TestSessionInvalidate(t *testing.T) {
	store := newSessionStore(1)
	token, _ := store.create("", User{})
	store.invalidate(token)
	assert.False(t, store.validate(token))
}

func TestSessionExpiry(t *testing.T) {
	store := &sessionStore{expiry: 1 * time.Millisecond}
	token, _ := store.create("", User{})
	time.Sleep(5 * time.Millisecond)
	assert.False(t, store.validate(token))
}

func TestSessionSweep(t *testing.T) {
	store := &sessionStore{expiry: 1 * time.Millisecond}
	token, _ := store.create("", User{})
	time.Sleep(5 * time.Millisecond)
	store.sweep()
	// After sweep, token should be gone from the map entirely
	_, loaded := store.sessions.Load(token)
	assert.False(t, loaded)
}

func TestSessionUnknownToken(t *testing.T) {
	store := newSessionStore(1)
	assert.False(t, store.validate("nonexistent"))
}

// --- Credential store ---

func TestCredentialSaveAndRetrieve(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := newCredentialStore(db, testLogger())

	exists, err := store.exists()
	require.NoError(t, err)
	assert.False(t, exists)

	cred := webauthn.Credential{
		ID:              []byte("test-cred-id"),
		PublicKey:       []byte("test-public-key"),
		AttestationType: "none",
		Authenticator: webauthn.Authenticator{
			AAGUID:    []byte("test-aaguid-1234"),
			SignCount: 0,
		},
	}

	err = store.save(cred, "did:key:zowner", mastodonAccount)
	require.NoError(t, err)

	exists, err = store.exists()
	require.NoError(t, err)
	assert.True(t, exists)

	creds, err := store.doorCredentials(NamespaceDefault)
	require.NoError(t, err)
	require.Len(t, creds, 1)
	assert.Equal(t, cred.ID, creds[0].ID)
	assert.Equal(t, cred.PublicKey, creds[0].PublicKey)
	assert.Equal(t, cred.AttestationType, creds[0].AttestationType)
	assert.Equal(t, cred.Authenticator.AAGUID, creds[0].Authenticator.AAGUID)
}

func TestCredentialUpdateSignCount(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := newCredentialStore(db, testLogger())

	cred := webauthn.Credential{
		ID:              []byte("sign-count-test"),
		PublicKey:       []byte("pub"),
		AttestationType: "none",
		Authenticator:   webauthn.Authenticator{AAGUID: []byte("aaguid"), SignCount: 5},
	}
	require.NoError(t, store.save(cred, "did:key:zowner", mastodonAccount))

	require.NoError(t, store.updateSignCount(cred.ID, 10))

	creds, err := store.doorCredentials(NamespaceDefault)
	require.NoError(t, err)
	assert.Equal(t, uint32(10), creds[0].Authenticator.SignCount)
}

// --- Middleware ---

func TestMiddlewareAllowsValidSession(t *testing.T) {
	sessions := newSessionStore(1)
	token, _ := sessions.create(mastodonAccount, User{})

	h := &Handler{sessions: sessions, logger: testLogger()}
	h.SetIdentities([]string{mastodonAccount}, nil)
	handler := h.Middleware("/test", everyLevel, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// An empty auth.root_identities admits nobody. A node with auth on and nobody
// listed is one no session may enter, not one any session may.
func TestAnEmptyListAdmitsNobody(t *testing.T) {
	sessions := newSessionStore(1)
	token, _ := sessions.create(mastodonAccount, User{})

	h := &Handler{sessions: sessions, logger: testLogger()}
	handler := h.Middleware("/test", everyLevel, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMiddlewareRedirectsPageRequest(t *testing.T) {
	sessions := newSessionStore(1)
	h := &Handler{sessions: sessions}
	handler := h.Middleware("/test", everyLevel, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusSeeOther, rec.Code)
	assert.Equal(t, "/auth/login?return=%2F", rec.Header().Get("Location"))
}

func TestMiddlewareRejectsAPIRequest(t *testing.T) {
	sessions := newSessionStore(1)
	h := &Handler{sessions: sessions}
	handler := h.Middleware("/test", everyLevel, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/timeseries/usage", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMiddlewareRejectsExpiredSession(t *testing.T) {
	sessions := &sessionStore{expiry: 1 * time.Millisecond}
	token, _ := sessions.create("", User{})
	time.Sleep(5 * time.Millisecond)

	h := &Handler{sessions: sessions}
	handler := h.Middleware("/test", everyLevel, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/timeseries/usage", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// --- Session cookie Secure flag ---

func TestSetSessionCookieSecureWhenConfigured(t *testing.T) {
	h := &Handler{secureCookies: true}
	rec := httptest.NewRecorder()
	h.setSessionCookie(rec, "tok")
	assertCookieSecure(t, rec, true)
}

func TestSetSessionCookieNotSecureByDefault(t *testing.T) {
	h := &Handler{secureCookies: false}
	rec := httptest.NewRecorder()
	h.setSessionCookie(rec, "tok")
	assertCookieSecure(t, rec, false)
}

func assertCookieSecure(t *testing.T, rec *httptest.ResponseRecorder, wantSecure bool) {
	t.Helper()
	setCookies := rec.Result().Cookies()
	require.Len(t, setCookies, 1)
	assert.Equal(t, wantSecure, setCookies[0].Secure, "cookie Secure flag mismatch")
}

// --- Bearer token path (ADR-025) ---

// memTokenStore is an in-memory TokenStore. ADR-025 specifies parquet and
// SQLite implementations as equals and neither exists yet (#827), so the
// endpoint and middleware contracts are exercised against this instead.
// Whatever implements TokenStore has to hold the same line: the raw token
// leaves once, only the hash is kept, revoked and expired tokens stop
// authenticating.
type memTokenStore struct {
	mu     sync.Mutex
	tokens map[string]*memToken // keyed by SHA-256 hash
	seq    int
	// touched is every hash presented, in order, so a test can see the use
	// being recorded.
	touched []string
}

type memToken struct {
	id        string
	label     string
	grant     Grant
	createdAt time.Time
	expiresAt *time.Time
	revoked   bool
}

func newMemTokenStore() *memTokenStore {
	return &memTokenStore{tokens: map[string]*memToken{}}
}

func (m *memTokenStore) Create(spec NewToken) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	id := fmt.Sprintf("AT_%d", m.seq)
	raw := fmt.Sprintf("qntx_%060d", m.seq)
	m.tokens[sha256Hex(raw)] = &memToken{
		id:    id,
		label: spec.Label,
		grant: Grant{
			Label:    spec.Label,
			DID:      fmt.Sprintf("did:key:ztoken%d", m.seq),
			MintedBy: spec.MintedBy,
			// The person the minting session named. A fake that dropped it made
			// every bearer look like a token nobody stands behind.
			MintedByUser:        spec.MintedByUser,
			MintedByDisplayName: spec.MintedByDisplayName,
			Level:               spec.Level,
			Namespaces:          spec.Namespaces,
			ReturnAddress:       spec.ReturnAddress,
		},
		createdAt: time.Now().UTC(),
		expiresAt: spec.ExpiresAt,
	}
	return raw, id, nil
}

func (m *memTokenStore) Issue(spec IssuedToken) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	id := fmt.Sprintf("AT_%d", m.seq)
	m.tokens[spec.Hash] = &memToken{
		id:    id,
		label: spec.Label,
		grant: Grant{
			DID:                 spec.DID,
			MintedBy:            spec.MintedBy,
			MintedByUser:        spec.MintedByUser,
			MintedByDisplayName: spec.MintedByDisplayName,
			Level:               spec.Level,
			Namespaces:          spec.Namespaces,
		},
		createdAt: time.Now().UTC(),
		expiresAt: spec.ExpiresAt,
	}
	return id, nil
}

// lookupOK is the bool the tests used to get, kept so they read as the
// authenticate-or-not question they are asking.
func (m *memTokenStore) lookupOK(hash string) bool {
	_, ok := m.Lookup(hash)
	return ok
}

func (m *memTokenStore) Lookup(hash string) (Grant, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	tok, ok := m.tokens[hash]
	if !ok || tok.revoked {
		return Grant{}, false
	}
	if tok.expiresAt != nil && time.Now().After(*tok.expiresAt) {
		return Grant{}, false
	}
	return tok.grant, true
}

func (m *memTokenStore) List() ([]TokenInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]TokenInfo, 0, len(m.tokens))
	for _, tok := range m.tokens {
		info := TokenInfo{
			ID:       tok.id,
			Label:    tok.label,
			DID:      tok.grant.DID,
			MintedBy: tok.grant.MintedBy,
			// Where a token may act is on the record it was minted from, so a
			// list that drops it cannot answer what was minted.
			Namespaces:    tok.grant.Namespaces,
			Level:         tok.grant.Level,
			ReturnAddress: tok.grant.ReturnAddress,
			CreatedAt:     tok.createdAt.Format(time.RFC3339Nano),
		}
		if tok.expiresAt != nil {
			expires := tok.expiresAt.UTC().Format(time.RFC3339Nano)
			info.ExpiresAt = &expires
		}
		if tok.revoked {
			// The real stores list a revoked token with when it stopped
			// working (ADR-025).
			revoked := time.Now().UTC().Format(time.RFC3339Nano)
			info.RevokedAt = &revoked
		}
		out = append(out, info)
	}
	return out, nil
}

func (m *memTokenStore) Revoke(id string) error {
	return m.setRevoked(id, true)
}

func (m *memTokenStore) Enable(id string) error {
	return m.setRevoked(id, false)
}

func (m *memTokenStore) Touch(hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.touched = append(m.touched, hash)
	return nil
}

func (m *memTokenStore) touchedHashes() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.touched...)
}

func (m *memTokenStore) setRevoked(id string, revoked bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, tok := range m.tokens {
		if tok.id == id {
			tok.revoked = revoked
		}
	}
	return nil
}

func TestMiddlewareAllowsValidBearerToken(t *testing.T) {
	store := newMemTokenStore()
	rawToken, _, err := store.Create(NewToken{Label: "laptop-cron", ExpiresAt: nil, MintedBy: mastodonAccount, Level: LevelAttestor})
	require.NoError(t, err)

	h := &Handler{
		sessions: newSessionStore(1),
		tokens:   store,
		logger:   testLogger(),
	}
	h.SetIdentities([]string{mastodonAccount}, nil)
	handler := h.Middleware("/test", everyLevel, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/timeseries/usage", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// Revocation is watched by last used (ADR-025), and nothing was writing it.
// Presenting a live token records the use, off the request's path.
func TestPresentingABearerRecordsItsUse(t *testing.T) {
	store := newMemTokenStore()
	rawToken, _, err := store.Create(NewToken{Label: "laptop-cron", MintedBy: mastodonAccount, Level: LevelAttestor})
	require.NoError(t, err)

	h := &Handler{sessions: newSessionStore(1), tokens: store, logger: testLogger()}
	h.SetIdentities([]string{mastodonAccount}, nil)
	handler := h.Middleware("/test", everyLevel, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	require.Eventually(t, func() bool {
		return len(store.touchedHashes()) == 1
	}, time.Second, 5*time.Millisecond, "the token was presented and its use was never recorded")
	assert.Equal(t, sha256Hex(rawToken), store.touchedHashes()[0])

	// A token nothing admits records nothing: there was no use.
	req = httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.Header.Set("Authorization", "Bearer qntx_nobody")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	assert.Len(t, store.touchedHashes(), 1)
}

// A token's glyph shows what the token may do, and that is one reading: the
// node's, the same one the gate makes for every request the token sends.
// The glyph asks here rather than working it out from the lines itself.
func TestGetTokenAnswersTheRolesAndWordsItHolds(t *testing.T) {
	store := newMemTokenStore()
	_, id, err := store.Create(NewToken{Label: "pond-sensor", MintedBy: mastodonAccount, Level: LevelAttestor, Namespaces: []string{"clean"}})
	require.NoError(t, err)
	h := &Handler{tokens: store, logger: testLogger()}
	h.SetIdentities([]string{mastodonAccount}, nil)
	h.SetRoleReader(&memRoles{
		lines: map[string][]RoleLine{"clean": {{
			Routes: []string{"pond-sensor"}, Roles: []string{"DATAPUNT"}, Granted: true, Actor: mastodonAccount, At: at(0),
		}}},
		words: []WordLine{
			{Write: true, Words: []string{"datapunt:observed"}, Roles: []string{"DATAPUNT"}, Actor: mastodonAccount, At: at(0)},
			{Write: true, Words: []string{"visit:done"}, Roles: []string{"WORKER"}, Actor: mastodonAccount, At: at(0)},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/auth/tokens/"+id, nil)
	rec := httptest.NewRecorder()
	h.handleTokenByID(rec, req, Presented{})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var answer tokenAnswer
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &answer))
	assert.Equal(t, id, answer.ID)
	assert.Equal(t, map[string][]string{"clean": {"DATAPUNT"}}, answer.Roles)
	assert.Equal(t, []string{"datapunt:observed"}, answer.Words.Write)
	assert.Equal(t, []string{}, answer.Words.Read)
	assert.Equal(t, map[string][]string{"DATAPUNT": {"datapunt:observed"}, "WORKER": {"visit:done"}}, answer.KnownRoles,
		"a grant cannot tell whether the role it names exists without this")

	req = httptest.NewRequest(http.MethodGet, "/auth/tokens/AT_nobody", nil)
	rec = httptest.NewRecorder()
	h.handleTokenByID(rec, req, Presented{})
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// "similar to ROOT yes, but SUPER can only list and read". A SUPER token
// reaches the token routes through the table and is served a GET; a write
// on the same routes wants a session, which a token never is.
func TestSuperListsAndReadsTokensAndChangesNone(t *testing.T) {
	store := newMemTokenStore()
	raw, id, err := store.Create(NewToken{Label: "SUPERANALYTICS", MintedBy: mastodonAccount, Level: LevelSuper})
	require.NoError(t, err)

	h := &Handler{
		tokens:   store,
		sessions: newSessionStore(1),
		logger:   testLogger(),
		corsWrap: func(handler http.HandlerFunc) http.HandlerFunc { return handler },
	}
	h.SetIdentities([]string{mastodonAccount}, nil)
	routes := h.Routes()

	// The route is answered on its table path; the request carries the whole.
	asSuper := func(method, route, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(`{"label":"another","level":"ATTESTOR"}`))
		req.Header.Set("Authorization", "Bearer "+raw)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		routes[route](rec, req)
		return rec
	}

	assert.Equal(t, http.StatusOK, asSuper(http.MethodGet, "/auth/tokens", "/auth/tokens").Code, "SUPER lists")
	one := asSuper(http.MethodGet, "/auth/tokens/", "/auth/tokens/"+id)
	assert.Equal(t, http.StatusOK, one.Code, "SUPER reads one: "+one.Body.String())
	assert.Contains(t, one.Body.String(), "SUPERANALYTICS")

	assert.Equal(t, http.StatusUnauthorized, asSuper(http.MethodPost, "/auth/tokens", "/auth/tokens").Code, "SUPER mints nothing")
	assert.Equal(t, http.StatusUnauthorized, asSuper(http.MethodDelete, "/auth/tokens/", "/auth/tokens/"+id).Code, "SUPER revokes nothing")
	listed, err := store.List()
	require.NoError(t, err)
	assert.Len(t, listed, 1, "a token was minted by a token")
	assert.False(t, store.tokens[sha256Hex(raw)].revoked, "a token was revoked by a token")
}

// --- Token endpoints (ADR-025) ---

func TestHandleCreateTokenReturnsRawOnce(t *testing.T) {
	store := newMemTokenStore()
	h := &Handler{tokens: store, logger: testLogger()}

	req := httptest.NewRequest(http.MethodPost, "/auth/tokens",
		strings.NewReader(`{"label":"laptop-cron","level":"ATTESTOR","scope":{"write":["ingested"]}}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	mint(h, rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, strings.HasPrefix(resp.Token, "qntx_"))
	assert.Equal(t, "laptop-cron", resp.Label)
	assert.True(t, store.lookupOK(sha256Hex(resp.Token)))
}

// "yes the label is the token's name". A grant hangs on it. Revoked ones count,
// since revocation is a switch and a switched-off token comes back.
func TestANameIsHeldByOneToken(t *testing.T) {
	store := newMemTokenStore()
	_, id, err := store.Create(NewToken{Label: "pond-sensor", MintedBy: mastodonAccount})
	require.NoError(t, err)
	require.NoError(t, store.Revoke(id))
	h := &Handler{tokens: store, logger: testLogger()}

	req := httptest.NewRequest(http.MethodPost, "/auth/tokens",
		strings.NewReader(`{"label":"pond-sensor","level":"ATTESTOR"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mint(h, rec, req)

	assert.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), id, "the answer names the token that holds the name")
	listed, err := store.List()
	require.NoError(t, err)
	assert.Len(t, listed, 1, "a second token was minted under the name")
}

func TestHandleListTokensExcludesRaw(t *testing.T) {
	store := newMemTokenStore()
	_, _, err := store.Create(NewToken{Label: "laptop-cron", ExpiresAt: nil})
	require.NoError(t, err)

	h := &Handler{tokens: store, logger: testLogger()}
	req := httptest.NewRequest(http.MethodGet, "/auth/tokens", nil)
	rec := httptest.NewRecorder()

	h.handleListTokens(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "qntx_")
	assert.NotContains(t, rec.Body.String(), "token_hash")
	assert.Contains(t, rec.Body.String(), "laptop-cron")
}

func TestHandleRevokeTokenBlocksFutureLookups(t *testing.T) {
	store := newMemTokenStore()
	raw, id, err := store.Create(NewToken{Label: "laptop-cron", ExpiresAt: nil})
	require.NoError(t, err)
	require.True(t, store.lookupOK(sha256Hex(raw)))

	h := &Handler{tokens: store, logger: testLogger()}
	req := httptest.NewRequest(http.MethodDelete, "/auth/tokens/"+id, nil)
	rec := httptest.NewRecorder()

	byID(h, rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.False(t, store.lookupOK(sha256Hex(raw)))
}

// The UI turns a token back on through this route, so the path has to reach
// enable rather than falling through to revoke.
func TestHandleEnableTokenRestoresIt(t *testing.T) {
	store := newMemTokenStore()
	raw, id, err := store.Create(NewToken{Label: "laptop-cron", ExpiresAt: nil})
	require.NoError(t, err)
	require.NoError(t, store.Revoke(id))
	require.False(t, store.lookupOK(sha256Hex(raw)))

	h := &Handler{tokens: store, logger: testLogger()}
	req := httptest.NewRequest(http.MethodPost, "/auth/tokens/"+id+"/enable", nil)
	rec := httptest.NewRecorder()

	byID(h, rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "enabled")
	assert.True(t, store.lookupOK(sha256Hex(raw)))
}

// A DELETE to the enable path must not revoke, and a POST to the bare id must
// not either — the two operations are opposites and the router decides which.
func TestTokenByIDRejectsWrongMethods(t *testing.T) {
	store := newMemTokenStore()
	_, id, err := store.Create(NewToken{Label: "laptop-cron", ExpiresAt: nil})
	require.NoError(t, err)

	h := &Handler{tokens: store, logger: testLogger()}

	for _, tc := range []struct{ method, path string }{
		{http.MethodDelete, "/auth/tokens/" + id + "/enable"},
		{http.MethodPost, "/auth/tokens/" + id},
	} {
		rec := httptest.NewRecorder()
		byID(h, rec, httptest.NewRequest(tc.method, tc.path, nil))
		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, "%s %s", tc.method, tc.path)
	}
}

// Revocation is a switch (ADR-025): kill the token, watch whether anything is
// still presenting it, turn it back on if that was you. Any TokenStore has to
// hold this line, not just the in-memory one.
func TestEnableRestoresARevokedToken(t *testing.T) {
	store := newMemTokenStore()
	raw, id, err := store.Create(NewToken{Label: "laptop-cron", ExpiresAt: nil})
	require.NoError(t, err)

	require.NoError(t, store.Revoke(id))
	require.False(t, store.lookupOK(sha256Hex(raw)))

	require.NoError(t, store.Enable(id))
	assert.True(t, store.lookupOK(sha256Hex(raw)))
}

// Enabling lifts a revocation. It is not a way to extend a lifetime.
func TestEnableDoesNotResurrectAnExpiredToken(t *testing.T) {
	store := newMemTokenStore()
	expired := time.Now().Add(-time.Hour)
	raw, id, err := store.Create(NewToken{Label: "laptop-cron", ExpiresAt: &expired})
	require.NoError(t, err)

	require.NoError(t, store.Revoke(id))
	require.NoError(t, store.Enable(id))

	assert.False(t, store.lookupOK(sha256Hex(raw)))
}
