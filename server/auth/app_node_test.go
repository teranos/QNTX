package auth

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "Why wouldn't the phone be one in the same way ? The App"
//
// k is a client of q: it signs as its own did:key, and q issues it a token for
// the person who said yes at the passkey. The App holds a laye key the same
// way, and the passkey at home enrols it as one.

const appDoor = "garden://door"

// appNode is a node with a door the App stands at, and a person whose User
// holds the App's laye key, as laye's sign-in leaves it.
func appNode(t *testing.T) (*Handler, *memTokenStore, *memUsers, ed25519.PrivateKey, string) {
	t.Helper()
	h := handlerWithDoors(t, gardenWithAnApp())
	h.configuredOrigin = nodeOrigin
	h.nodeKey = testNodeKey(t)
	store := newMemTokenStore()
	h.tokens = store
	key, did := otherNode(t)
	users := &memUsers{held: []User{{
		ID:       "US-1",
		Level:    LevelRoot,
		Keys:     []UserKey{{DID: did, Origin: OriginBrowser}},
		Accounts: []UserAccount{{Provider: "mastodon", CanonicalID: mastodonAccount}},
	}}}
	h.users = users
	return h, store, users, key, did
}

// signedInFrom is the way home from a door, ended by the passkey, standing on
// a half-admission laye proved with layeKey. It answers where the browser is
// sent next.
func signedInFrom(t *testing.T, h *Handler, door, layeKey string) string {
	t.Helper()
	w := httptest.NewRecorder()
	h.handleHomeward(w, httptest.NewRequest(http.MethodGet, homewardPath+"?door="+url.QueryEscape(door), nil))
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	ticket := homewardCookie(t, w)
	finish := httptest.NewRequest(http.MethodPost, "/auth/login/finish", nil)
	finish.AddCookie(&http.Cookie{Name: homewardCookieName, Value: ticket.Value})
	session, err := h.sessions.create(mastodonAccount, h.userFor(mastodonAccount))
	require.NoError(t, err)
	answer, err := h.finished(httptest.NewRecorder(), finish, session, mastodonAccount, layeKey)
	require.NoError(t, err)
	next, _ := answer["return"].(string)
	return next
}

// appCode runs a client's whole journey for the App, from its door, and
// answers the code sent back there with the verifier it was challenged by.
func appCode(t *testing.T, h *Handler, did string) (code, verifier string) {
	t.Helper()
	verifier, challenge := pkcePair()
	w := httptest.NewRecorder()
	h.handleAuthorize(w, authorizeRequest(did, appDoor, challenge))
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	ticket := homewardCookie(t, w)
	finish := httptest.NewRequest(http.MethodPost, "/auth/login/finish", nil)
	finish.AddCookie(&http.Cookie{Name: homewardCookieName, Value: ticket.Value})
	session, err := h.sessions.create(mastodonAccount, h.userFor(mastodonAccount))
	require.NoError(t, err)
	answer, err := h.finished(httptest.NewRecorder(), finish, session, mastodonAccount, "")
	require.NoError(t, err)
	done := httptest.NewRecorder()
	h.handleAuthorizeDone(done, httptest.NewRequest(http.MethodGet, answer["return"].(string), nil))
	require.Equal(t, http.StatusSeeOther, done.Code, done.Body.String())
	sent, err := url.Parse(done.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, appDoor, sent.Scheme+"://"+sent.Host+sent.Path)
	code = sent.Query().Get("code")
	require.NotEmpty(t, code)
	return code, verifier
}

// asItself is a form to the token endpoint, signed the way k signs one.
func asItself(t *testing.T, key ed25519.PrivateKey, did, jti string, form url.Values) *http.Request {
	t.Helper()
	form.Set("client_id", did)
	form.Set("client_assertion_type", clientAssertionType)
	form.Set("client_assertion", assertion(t, key, did, nodeOrigin+tokenPath, jti, time.Now().Add(time.Minute)))
	r := httptest.NewRequest(http.MethodPost, tokenPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

type issuedPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// The passkey at home writes the App's key as a client of this node, at the
// App's door, in the namespace that door opens onto.
func TestThePasskeyAtHomeEnrolsTheAppsKey(t *testing.T) {
	h, _, users, _, did := appNode(t)

	next := signedInFrom(t, h, appDoor, did)
	assert.True(t, strings.HasPrefix(next, appDoor+"?home="), next)

	client, ok := h.clientByDID(did)
	require.True(t, ok)
	assert.True(t, client.App)
	assert.False(t, client.Node)
	assert.Equal(t, appDoor, client.ReturnAddress)
	assert.Equal(t, "garden", client.Namespace)
	assert.Equal(t, appDoor, users.held[0].Keys[0].Door)
}

// A browser tab is reached by no scheme, so nothing is sent a code there.
func TestAKeySignedInAtAWebDoorIsNoClient(t *testing.T) {
	h, _, users, _, did := appNode(t)

	signedInFrom(t, h, "https://portal.garden.test", did)

	_, ok := h.clientByDID(did)
	assert.False(t, ok)
	assert.Empty(t, users.held[0].Keys[0].Door)
}

// Only a key the person already holds is enrolled. A half-admission is about
// one key, and the User it reached is the only one who can hold it.
func TestAKeyThePersonDoesNotHoldIsNotEnrolled(t *testing.T) {
	h, _, _, _, _ := appNode(t)
	_, stranger := otherNode(t)

	signedInFrom(t, h, appDoor, stranger)

	_, ok := h.clientByDID(stranger)
	assert.False(t, ok)
}

// Signed in the way k is: the code comes to the App's door, the App spends it
// signing as its key, and the token names the App as its client.
func TestTheAppSignsInTheWayKDoes(t *testing.T) {
	h, store, _, key, did := appNode(t)
	signedInFrom(t, h, appDoor, did)

	code, verifier := appCode(t, h, did)
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", appDoor)
	form.Set("code_verifier", verifier)
	w := httptest.NewRecorder()
	h.handleToken(w, asItself(t, key, did, "spend", form))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var answer issuedPair
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answer))
	require.NotEmpty(t, answer.AccessToken)
	grant, ok := store.Lookup(sha256Hex(answer.AccessToken))
	require.True(t, ok)
	assert.Equal(t, did, grant.ClientDID)
	assert.Equal(t, mastodonAccount, grant.MintedBy)
	assert.Equal(t, []string{"garden"}, grant.Namespaces)
}

// A phone is not there to do a passkey every hour. It stays signed in by its
// refresh token, spent signing as its key, so the token alone is not enough.
func TestTheAppStaysSignedInSigningAsItsKey(t *testing.T) {
	h, _, _, key, did := appNode(t)
	signedInFrom(t, h, appDoor, did)
	code, verifier := appCode(t, h, did)
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", appDoor)
	form.Set("code_verifier", verifier)
	w := httptest.NewRecorder()
	h.handleToken(w, asItself(t, key, did, "spend", form))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var first issuedPair
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &first))
	require.NotEmpty(t, first.RefreshToken)

	wrongKey, _ := otherNode(t)
	refresh := url.Values{}
	refresh.Set("grant_type", "refresh_token")
	refresh.Set("refresh_token", first.RefreshToken)
	stolen := httptest.NewRecorder()
	h.handleToken(stolen, asItself(t, wrongKey, did, "stolen", refresh))
	assert.Equal(t, http.StatusUnauthorized, stolen.Code, stolen.Body.String())

	refresh = url.Values{}
	refresh.Set("grant_type", "refresh_token")
	refresh.Set("refresh_token", first.RefreshToken)
	again := httptest.NewRecorder()
	h.handleToken(again, asItself(t, key, did, "refresh", refresh))
	require.Equal(t, http.StatusOK, again.Code, again.Body.String())
	var second issuedPair
	require.NoError(t, json.Unmarshal(again.Body.Bytes(), &second))
	assert.NotEmpty(t, second.AccessToken)
	assert.NotEqual(t, first.AccessToken, second.AccessToken)
}

// The App signs as itself and is told nothing a peer is told: the lines are
// for a node that serves a namespace.
func TestTheAppIsNotToldTheLines(t *testing.T) {
	h, _, _, key, did := appNode(t)
	signedInFrom(t, h, appDoor, did)

	form := url.Values{}
	form.Set("client_id", did)
	form.Set("client_assertion_type", clientAssertionType)
	form.Set("client_assertion", assertion(t, key, did, nodeOrigin+tokenPath, "lines", time.Now().Add(time.Minute)))
	r := httptest.NewRequest(http.MethodPost, linesPath, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.handleLines(w, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
}

// Forgetting the device takes the client with it: the key is the client.
func TestAKeyThePersonLetsGoOfIsNoClient(t *testing.T) {
	h, _, users, _, did := appNode(t)
	signedInFrom(t, h, appDoor, did)
	_, ok := h.clientByDID(did)
	require.True(t, ok)

	users.held[0].Keys = nil

	_, ok = h.clientByDID(did)
	assert.False(t, ok)
}

// A door struck out of am.toml enrols nobody and keeps nobody it enrolled.
func TestAnAppWhoseDoorIsGoneIsNoClient(t *testing.T) {
	h, _, _, _, did := appNode(t)
	signedInFrom(t, h, appDoor, did)

	require.NoError(t, h.SetDoors([]Door{garden()}))

	_, ok := h.clientByDID(did)
	assert.False(t, ok)
}

// The App proves its key in its own WebView and the passkey is done in the
// sheet, which has no half-admission of its own. The authorize endpoint takes
// it home the way the door's way home does.
func TestTheAppCarriesWhatItProvedToTheAuthorizeEndpoint(t *testing.T) {
	h, _, _, _, did := appNode(t)
	signedInFrom(t, h, appDoor, did)
	pending, err := h.pendingLogins.openWith(halfAdmission{identity: mastodonAccount, did: did})
	require.NoError(t, err)

	_, challenge := pkcePair()
	r := authorizeRequest(did, appDoor, challenge)
	q := r.URL.Query()
	q.Set("pending", pending)
	r.URL.RawQuery = q.Encode()
	w := httptest.NewRecorder()
	h.handleAuthorize(w, r)
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())

	var carried string
	for _, c := range w.Result().Cookies() {
		if c.Name == pendingCookieName {
			carried = c.Value
		}
	}
	assert.Equal(t, pending, carried)
}
