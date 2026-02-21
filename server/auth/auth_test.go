package auth

import (
	"net/http"
	"net/http/httptest"
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
