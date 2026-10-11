package auth

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A node whose people log in at another: its login sends them there, the
// passkey is done there, and the code that comes back is a session here.

// elsewhereOrigin is where the node whose people log in elsewhere answers.
const elsewhereOrigin = "https://elsewhere.node.test"

// loginElsewhere is the issuer serving its routes with the node declared at
// it, and the node logging its people in there.
func loginElsewhere(t *testing.T) (issuer, node *Handler) {
	t.Helper()
	issuer, _, key, _ := registered(t)
	mux := http.NewServeMux()
	for path, handle := range issuer.Routes() {
		mux.HandleFunc(path, handle)
	}
	served := httptest.NewServer(mux)
	t.Cleanup(served.Close)
	issuer.configuredOrigin = served.URL

	node = handlerWithDoors(t)
	node.tokens = newMemTokenStore()
	node.nodeKey = key
	node.configuredOrigin = elsewhereOrigin
	require.NoError(t, node.SetPrimary(served.URL))
	return issuer, node
}

// sentToTheIssuer is the node sending somebody to log in, and the issuer
// taking them through the passkey as mastodonAccount: where the issuer sends
// them back to.
func sentToTheIssuer(t *testing.T, issuer, node *Handler, going string) *url.URL {
	t.Helper()
	start := httptest.NewRecorder()
	node.handleElsewhere(start, httptest.NewRequest(http.MethodGet, elsewherePath+"?return="+url.QueryEscape(going), nil))
	require.Equal(t, http.StatusFound, start.Code, start.Body.String())
	there, err := url.Parse(start.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, issuer.publicOrigin()+authorizePath, there.Scheme+"://"+there.Host+there.Path)

	authorize := httptest.NewRecorder()
	issuer.handleAuthorize(authorize, httptest.NewRequest(http.MethodGet, authorizePath+"?"+there.RawQuery, nil))
	require.Equal(t, http.StatusFound, authorize.Code, authorize.Body.String())
	ticket := homewardCookie(t, authorize)

	finish := httptest.NewRequest(http.MethodPost, "/auth/login/finish", nil)
	finish.AddCookie(&http.Cookie{Name: homewardCookieName, Value: ticket.Value})
	session, err := issuer.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	next, _ := issuer.finished(httptest.NewRecorder(), finish, session, mastodonAccount)["return"].(string)

	done := httptest.NewRecorder()
	issuer.handleAuthorizeDone(done, httptest.NewRequest(http.MethodGet, next, nil))
	require.Equal(t, http.StatusSeeOther, done.Code, done.Body.String())
	back, err := url.Parse(done.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, elsewhereOrigin+elsewhereBackPath, back.Scheme+"://"+back.Host+back.Path)
	return back
}

func sessionCookieOf(w *httptest.ResponseRecorder) (*http.Cookie, bool) {
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c, true
		}
	}
	return nil, false
}

// The whole of it: logged in at the issuer, a session here, as the same person,
// back where they were going.
func TestSomebodyLoggedInAtTheIssuerHasASessionHere(t *testing.T) {
	issuer, node := loginElsewhere(t)
	back := sentToTheIssuer(t, issuer, node, "/canvas")

	w := httptest.NewRecorder()
	node.handleElsewhereBack(w, httptest.NewRequest(http.MethodGet, elsewhereBackPath+"?"+back.RawQuery, nil))
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	assert.Equal(t, "/canvas", w.Header().Get("Location"))

	cookie, set := sessionCookieOf(w)
	require.True(t, set, "no session was set")
	who, ok := node.sessions.identityOf(cookie.Value)
	require.True(t, ok)
	assert.Equal(t, mastodonAccount, who)
}

// The issuer says who somebody is. Whether they are let in is this node's list.
func TestTheNodesOwnListDecidesWhoLogsInElsewhere(t *testing.T) {
	issuer, node := loginElsewhere(t)
	back := sentToTheIssuer(t, issuer, node, "/")
	node.SetIdentities(nil, nil)

	w := httptest.NewRecorder()
	node.handleElsewhereBack(w, httptest.NewRequest(http.MethodGet, elsewhereBackPath+"?"+back.RawQuery, nil))
	assert.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	_, set := sessionCookieOf(w)
	assert.False(t, set, "a session was set for somebody this node does not admit")
}

// A code comes back once, to the journey this node started.
func TestTheWayBackAnswersOnlyAJourneyThisNodeStarted(t *testing.T) {
	issuer, node := loginElsewhere(t)
	back := sentToTheIssuer(t, issuer, node, "/")

	forged := back.Query()
	forged.Set("state", "nobody-started-this")
	w := httptest.NewRecorder()
	node.handleElsewhereBack(w, httptest.NewRequest(http.MethodGet, elsewhereBackPath+"?"+forged.Encode(), nil))
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	first := httptest.NewRecorder()
	node.handleElsewhereBack(first, httptest.NewRequest(http.MethodGet, elsewhereBackPath+"?"+back.RawQuery, nil))
	require.Equal(t, http.StatusSeeOther, first.Code, first.Body.String())
	again := httptest.NewRecorder()
	node.handleElsewhereBack(again, httptest.NewRequest(http.MethodGet, elsewhereBackPath+"?"+back.RawQuery, nil))
	assert.Equal(t, http.StatusNotFound, again.Code, again.Body.String())
}

// Where somebody goes after logging in is a place on this node.
func TestTheWayBackStaysOnThisNode(t *testing.T) {
	issuer, node := loginElsewhere(t)
	for _, going := range []string{"https://other.example/", "//other.example/", "canvas"} {
		back := sentToTheIssuer(t, issuer, node, going)
		w := httptest.NewRecorder()
		node.handleElsewhereBack(w, httptest.NewRequest(http.MethodGet, elsewhereBackPath+"?"+back.RawQuery, nil))
		require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
		assert.Equal(t, "/", w.Header().Get("Location"), going)
	}
}

// The login page is the issuer's, on a node that logs its people in there.
func TestTheLoginPageSendsSomebodyToTheIssuer(t *testing.T) {
	_, node := loginElsewhere(t)
	w := httptest.NewRecorder()
	node.handleLogin(w, httptest.NewRequest(http.MethodGet, "/auth/login?return=%2Fcanvas", nil))
	require.Equal(t, http.StatusSeeOther, w.Code)
	assert.Equal(t, elsewherePath+"?return=%2Fcanvas", w.Header().Get("Location"))
}

// A node that logs its people in here sends nobody elsewhere.
func TestANodeThatLogsInHereSendsNobodyElsewhere(t *testing.T) {
	node := handlerWithDoors(t)
	w := httptest.NewRecorder()
	node.handleElsewhere(w, httptest.NewRequest(http.MethodGet, elsewherePath, nil))
	assert.Equal(t, http.StatusNotFound, w.Code)

	page := httptest.NewRecorder()
	node.handleLogin(page, httptest.NewRequest(http.MethodGet, "/auth/login", nil))
	assert.Equal(t, http.StatusOK, page.Code)
}

// An origin that is not one is said at startup.
func TestALoginElsewhereThatIsNoOriginIsRefused(t *testing.T) {
	node := handlerWithDoors(t)
	assert.Error(t, node.SetPrimary("api.node.test"))
	assert.NoError(t, node.SetPrimary(""))
}
