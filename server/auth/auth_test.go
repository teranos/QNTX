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
	"github.com/teranos/errors"
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

	creds, err := store.getAll()
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

	creds, err := store.getAll()
	require.NoError(t, err)
	assert.Equal(t, uint32(10), creds[0].Authenticator.SignCount)
}

// --- Middleware ---

func TestMiddlewareAllowsValidSession(t *testing.T) {
	sessions := newSessionStore(1)
	token, _ := sessions.create(mastodonAccount, User{})

	h := &Handler{sessions: sessions, logger: testLogger()}
	h.SetIdentities([]string{mastodonAccount}, nil)
	handler := h.Middleware(func(w http.ResponseWriter, r *http.Request) {
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
	handler := h.Middleware(func(w http.ResponseWriter, r *http.Request) {
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
	handler := h.Middleware(func(w http.ResponseWriter, r *http.Request) {
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
	handler := h.Middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestMiddlewareRejectsExpiredSession(t *testing.T) {
	sessions := &sessionStore{expiry: 1 * time.Millisecond}
	token, _ := sessions.create("", User{})
	time.Sleep(5 * time.Millisecond)

	h := &Handler{sessions: sessions}
	handler := h.Middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
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
			DID:        fmt.Sprintf("did:key:ztoken%d", m.seq),
			MintedBy:   spec.MintedBy,
			Label:      spec.Label,
			Level:      spec.Level,
			Namespaces: spec.Namespaces,
			ScopeRead:  spec.ScopeRead,
			ScopeWrite: spec.ScopeWrite,
		},
		createdAt: time.Now().UTC(),
		expiresAt: spec.ExpiresAt,
	}
	return raw, id, nil
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
		out = append(out, TokenInfo{
			ID:    tok.id,
			Label: tok.label,
			// Where a token may act is on the record it was minted from, so a
			// list that drops it cannot answer what was minted.
			Namespaces: tok.grant.Namespaces,
			ScopeRead:  tok.grant.ScopeRead,
			ScopeWrite: tok.grant.ScopeWrite,
			CreatedAt:  tok.createdAt.Format(time.RFC3339Nano),
		})
	}
	return out, nil
}

func (m *memTokenStore) Revoke(id string) error {
	return m.setRevoked(id, true)
}

func (m *memTokenStore) Enable(id string) error {
	return m.setRevoked(id, false)
}

func (m *memTokenStore) SetScope(id string, read, write []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, tok := range m.tokens {
		if tok.id == id {
			tok.grant.ScopeRead = read
			tok.grant.ScopeWrite = write
			return nil
		}
	}
	return errors.Newf("no token matched %s on set scope", id)
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
	rawToken, _, err := store.Create(NewToken{Label: "laptop-cron", ExpiresAt: nil, MintedBy: mastodonAccount, ScopeRead: []string{"reads"}, ScopeWrite: []string{"writes"}})
	require.NoError(t, err)

	h := &Handler{
		sessions: newSessionStore(1),
		tokens:   store,
		logger:   testLogger(),
	}
	h.SetIdentities([]string{mastodonAccount}, nil)
	handler := h.Middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
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

func TestHandleListTokensExcludesRaw(t *testing.T) {
	store := newMemTokenStore()
	_, _, err := store.Create(NewToken{Label: "laptop-cron", ExpiresAt: nil, ScopeRead: []string{"reads"}, ScopeWrite: []string{"writes"}})
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
	raw, id, err := store.Create(NewToken{Label: "laptop-cron", ExpiresAt: nil, ScopeRead: []string{"reads"}, ScopeWrite: []string{"writes"}})
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
	raw, id, err := store.Create(NewToken{Label: "laptop-cron", ExpiresAt: nil, ScopeRead: []string{"reads"}, ScopeWrite: []string{"writes"}})
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
	_, id, err := store.Create(NewToken{Label: "laptop-cron", ExpiresAt: nil, ScopeRead: []string{"reads"}, ScopeWrite: []string{"writes"}})
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
	raw, id, err := store.Create(NewToken{Label: "laptop-cron", ExpiresAt: nil, ScopeRead: []string{"reads"}, ScopeWrite: []string{"writes"}})
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
	raw, id, err := store.Create(NewToken{Label: "laptop-cron", ExpiresAt: &expired, ScopeRead: []string{"reads"}, ScopeWrite: []string{"writes"}})
	require.NoError(t, err)

	require.NoError(t, store.Revoke(id))
	require.NoError(t, store.Enable(id))

	assert.False(t, store.lookupOK(sha256Hex(raw)))
}
