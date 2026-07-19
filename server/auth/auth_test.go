package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	token, err := store.create()
	require.NoError(t, err)
	assert.Len(t, token, 64) // 32 bytes hex
	assert.True(t, store.validate(token))
}

func TestSessionInvalidate(t *testing.T) {
	store := newSessionStore(1)
	token, _ := store.create()
	store.invalidate(token)
	assert.False(t, store.validate(token))
}

func TestSessionExpiry(t *testing.T) {
	store := &sessionStore{expiry: 1 * time.Millisecond}
	token, _ := store.create()
	time.Sleep(5 * time.Millisecond)
	assert.False(t, store.validate(token))
}

func TestSessionSweep(t *testing.T) {
	store := &sessionStore{expiry: 1 * time.Millisecond}
	token, _ := store.create()
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

	err = store.save(cred)
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
	require.NoError(t, store.save(cred))

	require.NoError(t, store.updateSignCount(cred.ID, 10))

	creds, err := store.getAll()
	require.NoError(t, err)
	assert.Equal(t, uint32(10), creds[0].Authenticator.SignCount)
}

// --- Middleware ---

func TestMiddlewareAllowsValidSession(t *testing.T) {
	sessions := newSessionStore(1)
	token, _ := sessions.create()

	h := &Handler{sessions: sessions}
	handler := h.Middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
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
	token, _ := sessions.create()
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

// --- Bearer token path (ADR-025) ---

// fakeTokenStore returns valid=true for one specific hash. Used in middleware
// tests where Create/List/Revoke are not exercised.
type fakeTokenStore struct{ acceptHash string }

func (f *fakeTokenStore) Lookup(hash string) bool { return hash == f.acceptHash }
func (f *fakeTokenStore) Create(label string, expiresAt *time.Time) (string, string, error) {
	return "", "", nil
}
func (f *fakeTokenStore) List() ([]TokenInfo, error) { return nil, nil }
func (f *fakeTokenStore) Revoke(id string) error     { return nil }

func TestMiddlewareAllowsValidBearerToken(t *testing.T) {
	rawToken := "qntx_deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdead"
	hash := sha256Hex(rawToken)

	h := &Handler{
		sessions: newSessionStore(1),
		tokens:   &fakeTokenStore{acceptHash: hash},
	}
	handler := h.Middleware(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.Header.Set("Authorization", "Bearer "+rawToken)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

// --- SQLite token store (ADR-025) ---

func TestSQLiteTokenStoreCreateAndLookup(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := newSQLiteTokenStore(db, testLogger())

	raw, _, err := store.Create("laptop-cron", nil)
	require.NoError(t, err)
	require.True(t, len(raw) > 5 && raw[:5] == "qntx_")

	assert.True(t, store.Lookup(sha256Hex(raw)))
	assert.False(t, store.Lookup(sha256Hex("qntx_wrong")))
}

func TestSQLiteTokenStoreRevoke(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := newSQLiteTokenStore(db, testLogger())

	raw, id, err := store.Create("laptop-cron", nil)
	require.NoError(t, err)
	require.True(t, store.Lookup(sha256Hex(raw)))

	require.NoError(t, store.Revoke(id))
	assert.False(t, store.Lookup(sha256Hex(raw)))
}

func TestSQLiteTokenStoreExpired(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := newSQLiteTokenStore(db, testLogger())

	past := time.Now().Add(-1 * time.Hour)
	raw, _, err := store.Create("laptop-cron", &past)
	require.NoError(t, err)

	assert.False(t, store.Lookup(sha256Hex(raw)))
}

func TestSQLiteTokenStoreList(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := newSQLiteTokenStore(db, testLogger())

	_, _, err := store.Create("laptop-cron", nil)
	require.NoError(t, err)
	_, revokedID, err := store.Create("old-ci", nil)
	require.NoError(t, err)
	require.NoError(t, store.Revoke(revokedID))

	infos, err := store.List()
	require.NoError(t, err)
	require.Len(t, infos, 2)

	labels := []string{infos[0].Label, infos[1].Label}
	assert.Contains(t, labels, "laptop-cron")
	assert.Contains(t, labels, "old-ci")

	// Response must never carry the raw token or hash.
	raw, _ := json.Marshal(infos)
	assert.NotContains(t, string(raw), "token_hash")
	assert.NotContains(t, string(raw), "qntx_")
}

// --- Token endpoints (ADR-025) ---

func TestHandleCreateTokenReturnsRawOnce(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := newSQLiteTokenStore(db, testLogger())
	h := &Handler{tokens: store, logger: testLogger()}

	req := httptest.NewRequest(http.MethodPost, "/auth/tokens",
		strings.NewReader(`{"label":"laptop-cron"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.handleCreateToken(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		ID    string `json:"id"`
		Label string `json:"label"`
		Token string `json:"token"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, strings.HasPrefix(resp.Token, "qntx_"))
	assert.Equal(t, "laptop-cron", resp.Label)
	assert.True(t, store.Lookup(sha256Hex(resp.Token)))
}

func TestHandleListTokensExcludesRaw(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := newSQLiteTokenStore(db, testLogger())
	_, _, err := store.Create("laptop-cron", nil)
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
	db := qntxtest.CreateTestDB(t)
	store := newSQLiteTokenStore(db, testLogger())
	raw, id, err := store.Create("laptop-cron", nil)
	require.NoError(t, err)
	require.True(t, store.Lookup(sha256Hex(raw)))

	h := &Handler{tokens: store, logger: testLogger()}
	req := httptest.NewRequest(http.MethodDelete, "/auth/tokens/"+id, nil)
	rec := httptest.NewRecorder()

	h.handleRevokeToken(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	assert.False(t, store.Lookup(sha256Hex(raw)))
}
