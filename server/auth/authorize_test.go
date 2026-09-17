package auth

import (
	"crypto/sha256"
	"encoding/base64"
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

// The authorize endpoint is the homeward journey with a client for a door:
// the client sends the person here, the passkey is done at home, and the
// code goes to the client's return address. The passkey is the yes.

const nodeOrigin = "https://api.node.test"

// A PKCE verifier and the S256 challenge made from it.
func pkcePair() (verifier, challenge string) {
	verifier = strings.Repeat("v", 43)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

// authorizingHandler is a node with its own door and one minted client.
func authorizingHandler(t *testing.T) (*Handler, *memTokenStore, string) {
	t.Helper()
	h := handlerWithDoors(t)
	h.configuredOrigin = nodeOrigin
	store := newMemTokenStore()
	h.tokens = store
	_, _, err := store.Create(NewToken{
		Label: "app", MintedBy: mastodonAccount, Level: LevelOAuth,
		Namespaces: []string{NamespaceDefault}, ReturnAddress: appReturn,
	})
	require.NoError(t, err)
	listed, err := store.List()
	require.NoError(t, err)
	return h, store, listed[0].DID
}

func authorizeRequest(did, redirect, challenge string) *http.Request {
	q := url.Values{}
	q.Set("client_id", did)
	q.Set("redirect_uri", redirect)
	q.Set("response_type", "code")
	q.Set("state", "state-of-the-app")
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("resource", nodeOrigin+"/mcp")
	return httptest.NewRequest(http.MethodGet, authorizePath+"?"+q.Encode(), nil)
}

// The whole journey: the client sends the person home, the passkey finishes,
// and the code arrives at the return address with the client's state.
func TestAClientSendsSomebodyHomeAndTheCodeGoesBack(t *testing.T) {
	h, _, did := authorizingHandler(t)
	_, challenge := pkcePair()

	w := httptest.NewRecorder()
	h.handleAuthorize(w, authorizeRequest(did, appReturn, challenge))
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	assert.Equal(t, h.homeOrigin()+"?homeward=1", w.Header().Get("Location"))
	ticket := homewardCookie(t, w)
	assert.True(t, ticket.HttpOnly)

	// The passkey finishes at home. The browser is told where to go next: the
	// node's own done page, by ticket, the way a door is named.
	finish := httptest.NewRequest(http.MethodPost, "/auth/login/finish", nil)
	finish.AddCookie(&http.Cookie{Name: homewardCookieName, Value: ticket.Value})
	session, err := h.sessions.create(mastodonAccount, User{ID: "US-1", DisplayName: "onf"})
	require.NoError(t, err)
	answer := h.finished(httptest.NewRecorder(), finish, session, mastodonAccount)
	next, _ := answer["return"].(string)
	require.Equal(t, nodeOrigin+authorizeDonePath+"?home="+url.QueryEscape(ticket.Value), next)

	done := httptest.NewRecorder()
	h.handleAuthorizeDone(done, httptest.NewRequest(http.MethodGet, next, nil))
	require.Equal(t, http.StatusSeeOther, done.Code, done.Body.String())
	sent, err := url.Parse(done.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "https://app.example/callback", sent.Scheme+"://"+sent.Host+sent.Path)
	assert.NotEmpty(t, sent.Query().Get("code"))
	assert.Equal(t, "state-of-the-app", sent.Query().Get("state"))
	assert.Empty(t, sent.Query().Get("error"))

	again := httptest.NewRecorder()
	h.handleAuthorizeDone(again, httptest.NewRequest(http.MethodGet, next, nil))
	assert.Equal(t, http.StatusNotFound, again.Code)
}

// The code carries who said yes, so the token endpoint can mint a token that
// is them. The passkey was done at home, which names no namespace.
func TestTheCodeCarriesWhoSaidYes(t *testing.T) {
	h, _, did := authorizingHandler(t)
	_, challenge := pkcePair()

	w := httptest.NewRecorder()
	h.handleAuthorize(w, authorizeRequest(did, appReturn, challenge))
	ticket := homewardCookie(t, w)
	finish := httptest.NewRequest(http.MethodPost, "/auth/login/finish", nil)
	finish.AddCookie(&http.Cookie{Name: homewardCookieName, Value: ticket.Value})
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	next := h.finished(httptest.NewRecorder(), finish, session, mastodonAccount)["return"].(string)
	done := httptest.NewRecorder()
	h.handleAuthorizeDone(done, httptest.NewRequest(http.MethodGet, next, nil))
	require.Equal(t, http.StatusSeeOther, done.Code, done.Body.String())

	store := h.oauthStore.Load()
	require.NotNil(t, store)
	store.mu.Lock()
	defer store.mu.Unlock()
	require.Len(t, store.codes, 1)
	for _, parked := range store.codes {
		carried, ok := parked.request.GetSession().(*TokenSession)
		require.True(t, ok, "the code's session is %T", parked.request.GetSession())
		assert.Equal(t, mastodonAccount, carried.MintedBy)
		assert.Empty(t, carried.Namespace)
		assert.Equal(t, did, parked.request.GetClient().GetID())
		assert.False(t, parked.spent)
	}
}

// A code goes only where the client was minted to go. A request naming any
// other address is refused before anyone is sent home.
func TestAnotherReturnAddressIsRefusedBeforeTheJourney(t *testing.T) {
	h, _, did := authorizingHandler(t)
	_, challenge := pkcePair()

	w := httptest.NewRecorder()
	h.handleAuthorize(w, authorizeRequest(did, "https://other.example/callback", challenge))

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	var refusal map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &refusal))
	assert.Equal(t, "invalid_request", refusal["error"])
	_, opened := h.homewards.Load(homewardCookieValue(w))
	assert.False(t, opened, "a journey was opened for a refused request")
}

func TestAnUnknownClientIsRefused(t *testing.T) {
	h, _, _ := authorizingHandler(t)
	_, challenge := pkcePair()

	w := httptest.NewRecorder()
	h.handleAuthorize(w, authorizeRequest("did:key:znobody", appReturn, challenge))

	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "invalid_client")
}

// PKCE is required of every client. Without it, the code is refused to the
// return address the request named, which is the client's own.
func TestAJourneyWithoutPKCEIsRefusedToTheClient(t *testing.T) {
	h, _, did := authorizingHandler(t)

	w := httptest.NewRecorder()
	h.handleAuthorize(w, authorizeRequest(did, appReturn, ""))

	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	sent, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "app.example", sent.Host)
	assert.Equal(t, "invalid_request", sent.Query().Get("error"))
	assert.Empty(t, sent.Query().Get("code"))
}

// A client revoked while the person was at the passkey is a door that shut
// behind them: no code.
func TestAClientRevokedMidJourneyGetsNoCode(t *testing.T) {
	h, store, did := authorizingHandler(t)
	_, challenge := pkcePair()

	w := httptest.NewRecorder()
	h.handleAuthorize(w, authorizeRequest(did, appReturn, challenge))
	ticket := homewardCookie(t, w)
	finish := httptest.NewRequest(http.MethodPost, "/auth/login/finish", nil)
	finish.AddCookie(&http.Cookie{Name: homewardCookieName, Value: ticket.Value})
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	next := h.finished(httptest.NewRecorder(), finish, session, mastodonAccount)["return"].(string)

	listed, err := store.List()
	require.NoError(t, err)
	require.NoError(t, store.Revoke(listed[0].ID))

	done := httptest.NewRecorder()
	h.handleAuthorizeDone(done, httptest.NewRequest(http.MethodGet, next, nil))
	assert.Equal(t, http.StatusUnauthorized, done.Code, done.Body.String())
}

// The done page answers only a ticket the journey handed out.
func TestTheDonePageAnswersOnlyATicket(t *testing.T) {
	h, _, _ := authorizingHandler(t)

	none := httptest.NewRecorder()
	h.handleAuthorizeDone(none, httptest.NewRequest(http.MethodGet, authorizeDonePath, nil))
	assert.Equal(t, http.StatusUnauthorized, none.Code)

	wrong := httptest.NewRecorder()
	h.handleAuthorizeDone(wrong, httptest.NewRequest(http.MethodGet, authorizeDonePath+"?home=nobody", nil))
	assert.Equal(t, http.StatusNotFound, wrong.Code)
}

// A journey nobody finished is swept, codes included.
func TestAnUnfinishedJourneyIsSwept(t *testing.T) {
	h, _, did := authorizingHandler(t)
	_, challenge := pkcePair()

	w := httptest.NewRecorder()
	h.handleAuthorize(w, authorizeRequest(did, appReturn, challenge))
	ticket := homewardCookie(t, w)

	val, ok := h.authorizings.Load(ticket.Value)
	require.True(t, ok)
	stale := val.(authorizing)
	stale.startedAt = time.Now().Add(-homewardTTL - time.Second)
	h.authorizings.Store(ticket.Value, stale)

	h.sweepAuthorizing()
	_, ok = h.authorizings.Load(ticket.Value)
	assert.False(t, ok)
}

func homewardCookieValue(w *httptest.ResponseRecorder) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == homewardCookieName {
			return c.Value
		}
	}
	return ""
}

// The face names the client. The page at home cannot read the ticket, so it
// asks the node who sent the person, and the node answers from the ticket.
func TestTheFaceIsToldWhoSentThePerson(t *testing.T) {
	h, _, did := authorizingHandler(t)
	_, challenge := pkcePair()

	w := httptest.NewRecorder()
	h.handleAuthorize(w, authorizeRequest(did, appReturn, challenge))
	ticket := homewardCookie(t, w)

	asked := httptest.NewRequest(http.MethodGet, journeyPath, nil)
	asked.AddCookie(&http.Cookie{Name: homewardCookieName, Value: ticket.Value})
	rec := httptest.NewRecorder()
	h.handleJourney(rec, asked)
	require.Equal(t, http.StatusOK, rec.Code)
	var journey map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &journey))
	assert.Equal(t, "client", journey["kind"])
	assert.Equal(t, "app", journey["label"], "the node's label for the client, not the id on the request")
	assert.NotContains(t, rec.Body.String(), did, "the face is told a name, not an id")
}

// A door's journey names the door, and no journey names nothing.
func TestTheFaceIsToldTheDoorOrNothing(t *testing.T) {
	h := handlerWithDoors(t, garden())

	w := httptest.NewRecorder()
	h.handleHomeward(w, wayHome("https://portal.garden.test/"))
	ticket := homewardCookie(t, w)

	asked := httptest.NewRequest(http.MethodGet, journeyPath, nil)
	asked.AddCookie(&http.Cookie{Name: homewardCookieName, Value: ticket.Value})
	rec := httptest.NewRecorder()
	h.handleJourney(rec, asked)
	require.Equal(t, http.StatusOK, rec.Code)
	var journey map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &journey))
	assert.Equal(t, "door", journey["kind"])
	assert.Equal(t, "https://portal.garden.test", journey["door"])

	nobody := httptest.NewRecorder()
	h.handleJourney(nobody, httptest.NewRequest(http.MethodGet, journeyPath, nil))
	require.Equal(t, http.StatusOK, nobody.Code)
	assert.JSONEq(t, `{}`, nobody.Body.String())

	stranger := httptest.NewRequest(http.MethodGet, journeyPath, nil)
	stranger.AddCookie(&http.Cookie{Name: homewardCookieName, Value: "not-a-ticket"})
	told := httptest.NewRecorder()
	h.handleJourney(told, stranger)
	assert.JSONEq(t, `{}`, told.Body.String())
}
