package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// A node where mastodonAccount holds a session, which is the only state that
// can make a code for a second device.
func connectHandler(t *testing.T) (*Handler, string) {
	t.Helper()
	h := &Handler{
		users:         &memUsers{},
		grants:        newDeviceGrants(qntxtest.CreateTestDB(t), zap.NewNop().Sugar()),
		sessions:      newSessionStore(24),
		pendingLogins: pendingLogins{},
		logger:        zap.NewNop().Sugar(),
	}
	h.SetIdentities([]string{mastodonAccount}, nil)

	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	return h, session
}

func mintCode(h *Handler, session string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/auth/connect", nil)
	if session != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	}
	rec := httptest.NewRecorder()
	h.handleConnect(rec, req)
	return rec
}

func redeem(h *Handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/auth/connect/redeem", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.handleConnectRedeem(rec, req)
	return rec
}

func ticketFrom(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Ticket string `json:"ticket"`
		Level  string `json:"level"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.Ticket
}

// Admitting a device is something an admitted device does.
func TestOnlyASignedInDeviceMakesACode(t *testing.T) {
	h, _ := connectHandler(t)

	assert.Equal(t, http.StatusForbidden, mintCode(h, "").Code)
	assert.Equal(t, http.StatusForbidden, mintCode(h, "not-a-session").Code)
}

// The code carries what the granting Caller was. Delegation never escalates,
// so the phone can be told what it is about to become.
func TestACodeCarriesTheGrantersLevel(t *testing.T) {
	h, session := connectHandler(t)

	rec := mintCode(h, session)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body struct {
		Ticket    string `json:"ticket"`
		Level     string `json:"level"`
		GrantDays int    `json:"grant_days"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.NotEmpty(t, body.Ticket)
	assert.Equal(t, string(LevelSuper), body.Level)
	assert.Equal(t, 30, body.GrantDays)
}

// A photograph of a screen is a copy. A code that could be redeemed twice
// admits everyone who saw it.
func TestACodeIsSpentOnce(t *testing.T) {
	h, session := connectHandler(t)
	ticket := ticketFrom(t, mintCode(h, session))

	first := redeem(h, `{"ticket":"`+ticket+`","did":"did:key:zPhone"}`)
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())

	second := redeem(h, `{"ticket":"`+ticket+`"}`)
	assert.Equal(t, http.StatusForbidden, second.Code)
}

// Redeeming hands back the right to enrol a passkey, and nothing else. No
// session cookie is written here: the finger is what finishes it.
func TestRedeemingOpensAHalfAdmission(t *testing.T) {
	h, session := connectHandler(t)
	ticket := ticketFrom(t, mintCode(h, session))

	rec := redeem(h, `{"ticket":"`+ticket+`","did":"did:key:zPhone"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var pending string
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == pendingCookieName {
			pending = cookie.Value
		}
		assert.NotEqual(t, sessionCookieName, cookie.Name, "redeeming must not sign anyone in")
	}
	require.NotEmpty(t, pending)

	admitted, ok := h.pendingLogins.peek(pending)
	require.True(t, ok)
	assert.Equal(t, mastodonAccount, admitted)
}

// A code minted before an account was struck out of am.toml does not outlive it.
func TestACodeDiesWithTheIdentityThatMadeIt(t *testing.T) {
	h, session := connectHandler(t)
	ticket := ticketFrom(t, mintCode(h, session))

	h.SetIdentities([]string{"https://elsewhere.example/@someone"}, nil)

	assert.Equal(t, http.StatusForbidden, redeem(h, `{"ticket":"`+ticket+`"}`).Code)
}

// An unknown code is refused, and so is one that names nothing.
func TestAnUnknownCodeIsRefused(t *testing.T) {
	h, _ := connectHandler(t)

	assert.Equal(t, http.StatusForbidden, redeem(h, `{"ticket":"made-up"}`).Code)
	assert.Equal(t, http.StatusForbidden, redeem(h, `{}`).Code)
}

// The grant is what makes the passkey the whole of a login, so a device with
// none is not one this node lets in on a fingerprint alone.
func TestOnlyALiveGrantMakesAPasskeyEnough(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	store := newDeviceGrants(db, zap.NewNop().Sugar())
	h := &Handler{
		creds:  newCredentialStore(db, zap.NewNop().Sugar()),
		grants: store,
		logger: zap.NewNop().Sugar(),
	}
	require.NoError(t, h.creds.save(credential("phone"), "did:key:zPhone", mastodonAccount))

	assert.False(t, h.granted([]byte("phone")), "a device nobody granted")

	require.NoError(t, store.record(deviceGrant{
		OwnerDID:   "did:key:zPhone",
		AdmittedAs: mastodonAccount,
		Level:      LevelSuper,
		GrantedBy:  mastodonAccount,
		ExpiresAt:  time.Now().Add(deviceGrantLife),
	}))
	assert.True(t, h.granted([]byte("phone")))

	// Thirty days is the grant's life. After it, the device asks for a new code.
	require.NoError(t, store.record(deviceGrant{
		OwnerDID:   "did:key:zPhone",
		AdmittedAs: mastodonAccount,
		Level:      LevelSuper,
		GrantedBy:  mastodonAccount,
		ExpiresAt:  time.Now().Add(-time.Minute),
	}))
	assert.False(t, h.granted([]byte("phone")))
}

// Beginning a ceremony asks whether any device was granted; a node holding none
// still refuses a passkey that stands on nothing.
func TestAPasskeyAloneNeedsSomeLiveGrant(t *testing.T) {
	db := qntxtest.CreateTestDB(t)
	h := &Handler{
		grants:        newDeviceGrants(db, zap.NewNop().Sugar()),
		pendingLogins: pendingLogins{},
		logger:        zap.NewNop().Sugar(),
	}
	bare := httptest.NewRequest(http.MethodPost, "/auth/login/begin", nil)

	assert.False(t, h.mayAssert(bare))

	require.NoError(t, h.grants.record(deviceGrant{
		OwnerDID:   "did:key:zPhone",
		AdmittedAs: mastodonAccount,
		Level:      LevelSuper,
		GrantedBy:  mastodonAccount,
		ExpiresAt:  time.Now().Add(deviceGrantLife),
	}))
	assert.True(t, h.mayAssert(bare))
}

// Scanning a second code is a renewal, not a second device.
func TestASecondCodeRenewsTheSameDevice(t *testing.T) {
	store := newDeviceGrants(qntxtest.CreateTestDB(t), zap.NewNop().Sugar())
	first := time.Now().Add(time.Hour)
	later := time.Now().Add(deviceGrantLife)

	require.NoError(t, store.record(deviceGrant{
		OwnerDID: "did:key:zPhone", AdmittedAs: mastodonAccount,
		Level: LevelSuper, GrantedBy: mastodonAccount, ExpiresAt: first,
	}))
	require.NoError(t, store.record(deviceGrant{
		OwnerDID: "did:key:zPhone", AdmittedAs: mastodonAccount,
		Level: LevelSuper, GrantedBy: mastodonAccount, ExpiresAt: later,
	}))

	held, found, err := store.of("did:key:zPhone")
	require.NoError(t, err)
	require.True(t, found)
	assert.WithinDuration(t, later, held.ExpiresAt, time.Second)
}
