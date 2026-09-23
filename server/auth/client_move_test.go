package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "I wish i could as ROOT, change the namespace where an OAUTH token is active in."

func moveRequest(id, body, session string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/auth/tokens/"+id+"/namespace", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if session != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	}
	return req
}

func clientID(t *testing.T, store *memTokenStore, did string) string {
	t.Helper()
	listed, err := store.List()
	require.NoError(t, err)
	for _, info := range listed {
		if info.DID == did && info.Level == LevelOAuth {
			return info.ID
		}
	}
	t.Fatal("no client in the store")
	return ""
}

func TestRootMovesAClientToAnotherNamespace(t *testing.T) {
	h, store, did := authorizingHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	id := clientID(t, store, did)

	rec := httptest.NewRecorder()
	byID(h, rec, moveRequest(id, `{"namespace":"pond"}`, session))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	client, ok := h.clientByDID(did)
	require.True(t, ok)
	assert.Equal(t, "pond", client.Namespace)
}

// The namespace a connector acts in is its client's, so a connector already in
// use follows the client at its next refresh, without the person again.
func TestAConnectorFollowsItsClientAtTheNextRefresh(t *testing.T) {
	h, store, did := authorizingHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	code, verifier := codeFor(t, h, did)
	secret := clientSecret(t, store, did)

	first := httptest.NewRecorder()
	h.handleToken(first, exchangeRequest(did, secret, code, verifier))
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())
	var was struct {
		Refresh string `json:"refresh_token"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &was))

	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	moved := httptest.NewRecorder()
	byID(h, moved, moveRequest(clientID(t, store, did), `{"namespace":"pond"}`, session))
	require.Equal(t, http.StatusOK, moved.Code, moved.Body.String())

	again := httptest.NewRecorder()
	h.handleToken(again, refreshRequest(did, secret, was.Refresh))
	require.Equal(t, http.StatusOK, again.Code, again.Body.String())
	var now struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(again.Body.Bytes(), &now))
	grant, live := store.Lookup(sha256Hex(now.AccessToken))
	require.True(t, live)
	assert.Equal(t, []string{"pond"}, grant.Namespaces, "the refresh kept the namespace the client was moved away from")
}

// Only a client is moved. A token of any other kind names where it acts at
// minting, and what it has written stays where it wrote it.
func TestOnlyAClientIsMoved(t *testing.T) {
	h, store := grantHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	_, id, err := store.Create(NewToken{Label: "cron", MintedBy: mastodonAccount, Level: LevelAttestor, Namespaces: []string{NamespaceDefault}})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	byID(h, rec, moveRequest(id, `{"namespace":"pond"}`, session))

	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}

// Naming a namespace is ROOT's (ADR-027), here as at minting.
func TestOnlyRootMovesAClient(t *testing.T) {
	h, store, did := authorizingHandler(t)
	h.SetIdentities(nil, nil)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	byID(h, rec, moveRequest(clientID(t, store, did), `{"namespace":"pond"}`, session))

	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
	client, ok := h.clientByDID(did)
	require.True(t, ok)
	assert.Equal(t, NamespaceDefault, client.Namespace)
}

// "A TOKEN CAN BE PUT INTO SOME NAMESPACE / A TOKEN CAN BE TAKEN OUT OF IT /
// ROOT CAN DO THIS / A TOKEN CAN ONLY BE ACTIVE IN ONE NAMESPACE AT A TIME"
func placeRequest(method, id, namespace, session string) *http.Request {
	var req *http.Request
	if method == http.MethodPost {
		req = httptest.NewRequest(method, "/auth/tokens/"+id+"/namespaces", strings.NewReader(`{"namespace":"`+namespace+`"}`))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, "/auth/tokens/"+id+"/namespaces/"+namespace, nil)
	}
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	return req
}

func namespacesOfClient(t *testing.T, store *memTokenStore, id string) []string {
	t.Helper()
	listed, err := store.List()
	require.NoError(t, err)
	for _, info := range listed {
		if info.ID == id {
			return info.Namespaces
		}
	}
	t.Fatal("no such token")
	return nil
}

func TestAClientIsPutIntoANamespaceWithoutBeingMovedThere(t *testing.T) {
	h, store, did := authorizingHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	id := clientID(t, store, did)

	rec := httptest.NewRecorder()
	byID(h, rec, placeRequest(http.MethodPost, id, "pond", session))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, []string{NamespaceDefault, "pond"}, namespacesOfClient(t, store, id))
	client, ok := h.clientByDID(did)
	require.True(t, ok)
	assert.Equal(t, NamespaceDefault, client.Namespace, "putting it in pond made it active there")
}

func TestMakingANamespaceActiveKeepsTheOthers(t *testing.T) {
	h, store, did := authorizingHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	id := clientID(t, store, did)
	put := httptest.NewRecorder()
	byID(h, put, placeRequest(http.MethodPost, id, "pond", session))
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())

	rec := httptest.NewRecorder()
	byID(h, rec, moveRequest(id, `{"namespace":"pond"}`, session))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, []string{"pond", NamespaceDefault}, namespacesOfClient(t, store, id))
	client, ok := h.clientByDID(did)
	require.True(t, ok)
	assert.Equal(t, "pond", client.Namespace)
}

func TestAClientIsTakenOutOfANamespaceItIsNotActiveIn(t *testing.T) {
	h, store, did := authorizingHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	id := clientID(t, store, did)
	put := httptest.NewRecorder()
	byID(h, put, placeRequest(http.MethodPost, id, "pond", session))
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())

	rec := httptest.NewRecorder()
	byID(h, rec, placeRequest(http.MethodDelete, id, "pond", session))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.Equal(t, []string{NamespaceDefault}, namespacesOfClient(t, store, id))
}

// No fallback: a token taken out of where it is active would be active
// nowhere, so the namespace it is active in stays until another is.
func TestTheNamespaceAClientIsActiveInIsNotTakenAway(t *testing.T) {
	h, store, did := authorizingHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)
	id := clientID(t, store, did)

	rec := httptest.NewRecorder()
	byID(h, rec, placeRequest(http.MethodDelete, id, NamespaceDefault, session))

	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	assert.Equal(t, []string{NamespaceDefault}, namespacesOfClient(t, store, id))
}

func TestAClientIsMovedToOneNamespace(t *testing.T) {
	h, store, did := authorizingHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	byID(h, rec, moveRequest(clientID(t, store, did), `{"namespace":""}`, session))

	assert.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
}
