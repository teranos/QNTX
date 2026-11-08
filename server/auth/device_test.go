package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agentKey is a key an agent holds and never hands over.
func agentKey(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return EncodeDIDKey(pub)
}

// askForSignIn is an agent asking for a sign-in under label, naming its did:key.
func askForSignIn(t *testing.T, h *Handler, label, did string) map[string]any {
	t.Helper()
	form := url.Values{"label": {label}, "did": {did}}
	r := httptest.NewRequest(http.MethodPost, deviceAuthorizationPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.handleDeviceAuthorization(w, r)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var asked map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &asked))
	return asked
}

// pollForToken is the agent asking the token endpoint whether its yes came.
func pollForToken(t *testing.T, h *Handler, deviceCode string) (int, map[string]any) {
	t.Helper()
	form := url.Values{"grant_type": {deviceCodeGrant}, "device_code": {deviceCode}}
	r := httptest.NewRequest(http.MethodPost, tokenPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.handleToken(w, r)
	var said map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &said), w.Body.String())
	return w.Code, said
}

// "yes, i want this"
// The agent prints a link, the person says yes at the passkey, and the agent's
// key is that person: ROOT here, since the passkey is a root identity's.
//
// "A signature proves the key, and nothing reusable leaves the node."
func TestAnAgentIsSignedInByThePersonsPasskey(t *testing.T) {
	h, store, _ := authorizingHandler(t)
	did := agentKey(t)
	asked := askForSignIn(t, h, "Coding Agent on l", did)
	deviceCode, _ := asked["device_code"].(string)
	userCode, _ := asked["user_code"].(string)
	require.NotEmpty(t, deviceCode)
	require.Len(t, userCode, userCodeLength+1)
	assert.Equal(t, nodeOrigin+deviceApprovePath+"?user_code="+url.QueryEscape(userCode), asked["verification_uri_complete"])

	code, said := pollForToken(t, h, deviceCode)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "authorization_pending", said["error"])

	// The person follows the link, typed as a person types it, and is sent home.
	typed := strings.ToLower(strings.ReplaceAll(userCode, "-", ""))
	w := httptest.NewRecorder()
	h.handleDeviceApprove(w, httptest.NewRequest(http.MethodGet, deviceApprovePath+"?user_code="+typed, nil))
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	assert.Equal(t, h.homeOrigin()+"?homeward=1", w.Header().Get("Location"))
	ticket := homewardCookie(t, w)

	// Home names who will hold the token, with the code to compare.
	face := httptest.NewRequest(http.MethodGet, journeyPath, nil)
	face.AddCookie(&http.Cookie{Name: homewardCookieName, Value: ticket.Value})
	named := httptest.NewRecorder()
	h.handleJourney(named, face)
	assert.JSONEq(t, `{"kind":"client","label":"Coding Agent on l `+userCode+`"}`, named.Body.String())

	finish := httptest.NewRequest(http.MethodPost, "/auth/login/finish", nil)
	finish.AddCookie(&http.Cookie{Name: homewardCookieName, Value: ticket.Value})
	session, err := h.sessions.create(mastodonAccount, User{ID: "US-1", DisplayName: "onf"})
	require.NoError(t, err)
	next, _ := h.finished(httptest.NewRecorder(), finish, session, mastodonAccount)["return"].(string)
	require.Equal(t, nodeOrigin+deviceDonePath+"?home="+url.QueryEscape(ticket.Value), next)
	done := httptest.NewRecorder()
	h.handleDeviceDone(done, httptest.NewRequest(http.MethodGet, next, nil))
	require.Equal(t, http.StatusOK, done.Code, done.Body.String())

	code, said = pollForToken(t, h, deviceCode)
	require.Equal(t, http.StatusOK, code, said)
	assert.NotContains(t, said, "access_token", "a sign-in handed the agent a token")
	assert.Equal(t, did, said["did"])

	grant, ok := store.Lookup(signedOnly(did))
	require.True(t, ok, "the person's yes was not held for the agent's key")
	admitted, in := h.admissionOf(Presented{Bearer: &grant})
	require.True(t, in)
	assert.Equal(t, string(LevelRoot), admitted.LevelName())
	assert.Equal(t, mastodonAccount, admitted.Identity)

	code, said = pollForToken(t, h, deviceCode)
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "invalid_grant", said["error"], "a sign-in is answered once")
}

// An agent that names no key of its own has nothing to sign with.
func TestASignInNamingNoKeyIsRefused(t *testing.T) {
	h, _, _ := authorizingHandler(t)
	for _, did := range []string{"", "not-a-did", "did:web:example.com"} {
		form := url.Values{"label": {"Coding Agent on l"}, "did": {did}}
		r := httptest.NewRequest(http.MethodPost, deviceAuthorizationPath, strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		h.handleDeviceAuthorization(w, r)
		assert.Equal(t, http.StatusBadRequest, w.Code, did)
		assert.Contains(t, w.Body.String(), "did", did)
	}
}

// An agent that does not say who it is is not shown to anybody.
func TestASignInNamingNoAgentIsRefused(t *testing.T) {
	h, _, _ := authorizingHandler(t)
	r := httptest.NewRequest(http.MethodPost, deviceAuthorizationPath, strings.NewReader(""))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.handleDeviceAuthorization(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "label")
}

// A code nothing waits under sends nobody home.
func TestALinkNoSignInWaitsUnderSendsNobodyHome(t *testing.T) {
	h, _, _ := authorizingHandler(t)
	w := httptest.NewRecorder()
	h.handleDeviceApprove(w, httptest.NewRequest(http.MethodGet, deviceApprovePath+"?user_code=BCDF-GHJK", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Empty(t, w.Header().Get("Location"))
}

// A device code nothing was issued under is over, in the words RFC 8628 gives.
func TestAnUnknownDeviceCodeIsExpired(t *testing.T) {
	h, _, _ := authorizingHandler(t)
	code, said := pollForToken(t, h, "not-a-device-code")
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Equal(t, "expired_token", said["error"])
}
