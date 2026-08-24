package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func statusOfRequest(t *testing.T, h *Handler, req *http.Request) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	h.handleStatus(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

func bearerRequest(t *testing.T, h *Handler, mintedBy string) *http.Request {
	t.Helper()
	store := newMemTokenStore()
	h.tokens = store
	raw, _, err := store.Create(NewToken{Label: "dev", MintedBy: mintedBy})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/auth/status", nil)
	req.Header.Set("Authorization", "Bearer "+raw)
	return req
}

// The door is drawn on identity alone (web/ts/signin.ts). A caller holding a
// token Middleware already grants SUPER to is somebody, and status saying
// nobody sent it to a passkey prompt that a token can never answer.
func TestStatusNamesTheIdentityABearerSpeaksFor(t *testing.T) {
	h := handlerWithCreds(t)
	h.SetIdentities([]string{mastodonAccount}, nil)

	req := bearerRequest(t, h, mastodonAccount)

	assert.Equal(t, mastodonAccount, statusOfRequest(t, h, req)["identity"])
}

// A token speaks for whoever minted it, so striking them out of am.toml has to
// reach status too — the same question Middleware asks before honouring one.
func TestStatusRefusesABearerWhoseMinterIsNoLongerListed(t *testing.T) {
	h := handlerWithCreds(t)
	h.SetIdentities(nil, nil)

	req := bearerRequest(t, h, mastodonAccount)

	assert.Equal(t, "", statusOfRequest(t, h, req)["identity"])
}

// A token minted by a half-admission names nobody, and nobody is not an
// identity to report however live the token is.
func TestStatusNamesNobodyForATokenThatNamesNobody(t *testing.T) {
	h := handlerWithCreds(t)
	h.SetIdentities([]string{mastodonAccount}, nil)

	req := bearerRequest(t, h, "")

	assert.Equal(t, "", statusOfRequest(t, h, req)["identity"])
}

// A session is still the answer where there is one, unchanged.
func TestStatusPrefersTheSessionItWasAlreadyReporting(t *testing.T) {
	h := handlerWithCreds(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/auth/status", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})

	assert.Equal(t, mastodonAccount, statusOfRequest(t, h, req)["identity"])
}

// No credential at all is still nobody, which is what draws the door.
func TestStatusNamesNobodyWithoutACredential(t *testing.T) {
	h := handlerWithCreds(t)
	req := httptest.NewRequest(http.MethodGet, "/auth/status", nil)
	assert.Equal(t, "", statusOfRequest(t, h, req)["identity"])
}
