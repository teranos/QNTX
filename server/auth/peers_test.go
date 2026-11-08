package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The primary carries a request for a peer to it, and the peer decides who is in.

// heard is what a peer was sent.
type heard struct {
	path, query, authorization, cookie string
}

// primaryAndPeer is a primary serving its routes, and a peer by peerDID it
// carries requests to, that answers with what it heard.
func primaryAndPeer(t *testing.T, peerDID string) (primary *Handler, served string, got *heard) {
	t.Helper()
	primary, _, _ = authorizingHandler(t)
	mux := http.NewServeMux()
	for path, handle := range primary.Routes() {
		mux.HandleFunc(path, handle)
	}
	primaryServed := httptest.NewServer(mux)
	t.Cleanup(primaryServed.Close)
	primary.configuredOrigin = primaryServed.URL

	got = &heard{}
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = heard{path: r.URL.Path, query: r.URL.RawQuery, authorization: r.Header.Get("Authorization"), cookie: r.Header.Get("Cookie")}
		http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "the-peers-own"})
		w.Header().Set("Access-Control-Allow-Origin", "https://anywhere.example")
		w.WriteHeader(http.StatusTeapot)
	}))
	t.Cleanup(peer.Close)
	require.NoError(t, primary.DeclarePeer(peerName, peerDID, []string{NamespaceDefault}, peer.URL, peer.URL))
	return primary, primaryServed.URL, got
}

// anyPeer is a primary and a peer whose key nothing signs with.
func anyPeer(t *testing.T) (*Handler, *heard, string) {
	t.Helper()
	_, did := otherNode(t)
	primary, _, got := primaryAndPeer(t, did)
	return primary, got, did
}

func toPeer(path string) *http.Request {
	return httptest.NewRequest(http.MethodGet, peerPath+peerName+path, nil)
}

// Somebody with a session here reaches the peer as themselves, and the session
// 「[Opus 5.5] cookie stays here. The primary signs for them as itself, and mints nothing.」
//
// "「[Sonnet 5.5] A signature proves the key, and nothing reusable leaves the node.」"
func TestASessionIsCarriedToThePeerAsThePerson(t *testing.T) {
	primary, got, _ := anyPeer(t)
	before, err := primary.tokens.List()
	require.NoError(t, err)
	session, err := primary.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	r := toPeer("/api/attestations?limit=1")
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	w := httptest.NewRecorder()
	primary.handlePeer(w, r)
	require.Equal(t, http.StatusTeapot, w.Code, w.Body.String())

	assert.Equal(t, "/api/attestations", got.path)
	assert.Equal(t, "limit=1", got.query)
	assert.Empty(t, got.cookie, "the session cookie left the primary")
	first, signed := strings.CutPrefix(got.authorization, signedScheme)
	require.True(t, signed, "the person was not carried by a signature: %q", got.authorization)

	again := toPeer("/health")
	again.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	primary.handlePeer(httptest.NewRecorder(), again)
	second, _ := strings.CutPrefix(got.authorization, signedScheme)
	assert.NotEqual(t, first, second, "one signature was carried twice")

	after, err := primary.tokens.List()
	require.NoError(t, err)
	assert.Len(t, after, len(before), "carrying a person minted a token")
}

// The peer admits the person it was carried, by asking the primary and by its own list.
func TestThePeerAdmitsThePersonCarried(t *testing.T) {
	key, did := otherNode(t)
	primary, served, got := primaryAndPeer(t, did)
	session, err := primary.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	r := toPeer("/api/attestations")
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	primary.handlePeer(httptest.NewRecorder(), r)

	node := handlerWithDoors(t)
	node.tokens = newMemTokenStore()
	node.nodeKey = key
	require.NoError(t, node.SetPrimary(served))
	carriedReq := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	carriedReq.Header.Set("Authorization", got.authorization)
	admission, admitted := node.admissionOf(node.presented(carriedReq))
	require.True(t, admitted, "the peer did not admit the person the primary carried")
	assert.Equal(t, mastodonAccount, admission.Identity)

	again := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	again.Header.Set("Authorization", got.authorization)
	_, admitted = node.admissionOf(node.presented(again))
	assert.False(t, admitted, "a carried signature was admitted twice")
}

// 「[Opus 5.5] A node that is not the peer named is not reached by what the primary carried there.」
func TestACarriedSignatureIsForThePeerItNames(t *testing.T) {
	_, did := otherNode(t)
	primary, served, got := primaryAndPeer(t, did)
	session, err := primary.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	r := toPeer("/api/attestations")
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	primary.handlePeer(httptest.NewRecorder(), r)

	elsewhere, _ := otherNode(t)
	node := handlerWithDoors(t)
	node.tokens = newMemTokenStore()
	node.nodeKey = elsewhere
	require.NoError(t, node.SetPrimary(served))
	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	req.Header.Set("Authorization", got.authorization)
	_, admitted := node.admissionOf(node.presented(req))
	assert.False(t, admitted)
}

// The person the primary carried is this node's own User for their route, or
// none when it keeps none: the primary's User is the primary's record.
func TestThePeerAdmitsThePersonCarriedAsItsOwnUser(t *testing.T) {
	key, did := otherNode(t)
	primary, served, got := primaryAndPeer(t, did)
	session, err := primary.sessions.create(mastodonAccount, User{ID: "US-AT-THE-PRIMARY"})
	require.NoError(t, err)
	r := toPeer("/api/attestations")
	r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	primary.handlePeer(httptest.NewRecorder(), r)

	carried := func(node *Handler) Admission {
		node.tokens = newMemTokenStore()
		node.nodeKey = key
		require.NoError(t, node.SetPrimary(served))
		req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
		req.Header.Set("Authorization", got.authorization)
		admission, admitted := node.admissionOf(node.presented(req))
		require.True(t, admitted)
		return admission
	}

	keepsNone := carried(handlerWithDoors(t))
	assert.Empty(t, keepsNone.UserID, "the primary's User was named on a node that does not hold it")

	holding := handlerWithDoors(t)
	holding.users = &memUsers{held: []User{{ID: "US-HERE", Level: LevelRoot, Accounts: []UserAccount{{Provider: "mastodon", CanonicalID: mastodonAccount}}}}}
	assert.Equal(t, "US-HERE", carried(holding).UserID)
}

// A request that carries its own token goes as it came: the peer reads it.
func TestABearerGoesToThePeerAsItCame(t *testing.T) {
	primary, got, _ := anyPeer(t)
	r := toPeer("/api/attestations")
	r.Header.Set("Authorization", "Bearer qntx_the-peers-own-token")
	primary.handlePeer(httptest.NewRecorder(), r)
	assert.Equal(t, "Bearer qntx_the-peers-own-token", got.authorization)
}

// A peer's session is its own, and which origins read an answer is said once, here.
func TestThePeersCookiesAndCORSStayWithThePeer(t *testing.T) {
	primary, _, _ := anyPeer(t)
	w := httptest.NewRecorder()
	primary.handlePeer(w, toPeer("/"))
	assert.Empty(t, w.Result().Cookies())
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

// A peer nobody named is not reached.
func TestAPeerNobodyNamedIsNotReached(t *testing.T) {
	primary, got, _ := anyPeer(t)
	w := httptest.NewRecorder()
	primary.handlePeer(w, httptest.NewRequest(http.MethodGet, peerPath+"nobody/api/attestations", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Empty(t, got.path)
}
