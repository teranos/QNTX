package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	qntxtest "github.com/teranos/QNTX/internal/testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Erasure is destructive and it is a right, so what it reaches, who may ask for
// it, and what it cannot reach are all worth stating.

const (
	erasedRoute  = "https://mastodon.example/@ana"
	erasedUserID = "US-USER-ERASEDONE"
	erasedBrowse = "did:key:zAnaBrowser"
	erasedDevice = "did:key:zAnaDevice"
)

// A node with a person on it: a public registration who arrived at a door,
// said a name and an address, logged in from a browser, enrolled a device, and
// whose ROOT is somebody else.
func erasingNode(t *testing.T) (*Handler, *memUsers, *memTokenStore) {
	t.Helper()

	users := &memUsers{}
	tokens := newMemTokenStore()
	h := &Handler{
		creds:    newCredentialStore(qntxtest.CreateTestDB(t), testLogger()),
		users:    users,
		tokens:   tokens,
		sessions: newSessionStore(24),
		logger:   testLogger(),
	}
	h.SetIdentities([]string{mastodonAccount}, nil)

	require.NoError(t, users.Put(User{
		ID:       "US-USER-THEOWNER",
		Level:    LevelRoot,
		Accounts: []UserAccount{{Provider: "mastodon", CanonicalID: mastodonAccount}},
	}))
	require.NoError(t, users.Put(User{
		ID:             erasedUserID,
		DisplayName:    "ana",
		EmailAddresses: []string{"ana@example.com"},
		Level:          LevelPublicRegistration,
		Namespace:      "garden",
		Keys: []UserKey{
			{DID: erasedBrowse, Origin: OriginBrowser},
			{DID: erasedDevice, Origin: OriginDevice},
		},
		Accounts: []UserAccount{{Provider: "mastodon", CanonicalID: erasedRoute, Handle: "@ana@mastodon.example"}},
	}))

	require.NoError(t, h.creds.saveAt(credential("ana-laptop"), erasedDevice, erasedRoute, "garden"))
	require.NoError(t, h.creds.saveAt(credential("owner-laptop"), "did:key:zOwner", mastodonAccount, NamespaceDefault))
	return h, users, tokens
}

// A session the person holds, resolved to their User the way login resolves it.
func sessionFor(t *testing.T, h *Handler, route string) string {
	t.Helper()
	token, err := h.sessions.create(route, h.userFor(route))
	require.NoError(t, err)
	return token
}

func eraseSelf(h *Handler, session string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/auth/user", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	rec := httptest.NewRecorder()
	h.sessionOnly(h.handleEraseSelf)(rec, req)
	return rec
}

func eraseByID(h *Handler, session, id string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/auth/users/"+id, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	rec := httptest.NewRecorder()
	h.sessionOnly(h.handleEraseByID)(rec, req)
	return rec
}

func erasureOf(t *testing.T, rec *httptest.ResponseRecorder) Erasure {
	t.Helper()
	var said Erasure
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &said), rec.Body.String())
	return said
}

// The right belongs to the person. Being logged in is the whole of what it
// takes, and everything the node holds that names them goes with it.
func TestAPersonErasesThemselvesAndEverythingThatNamedThem(t *testing.T) {
	h, users, tokens := erasingNode(t)
	kept := &memAttestor{}
	h.SetAttestor(kept)

	raw, _, err := tokens.Create(NewToken{
		Label: "ana's cron", MintedBy: erasedRoute,
		MintedByUser: erasedUserID, MintedByDisplayName: "ana", Level: LevelAttestor,
	})
	require.NoError(t, err)

	session := sessionFor(t, h, erasedRoute)
	other := sessionFor(t, h, erasedRoute)

	rec := eraseSelf(h, session)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	// The User record.
	_, found, err := users.ByRoute(erasedRoute)
	require.NoError(t, err)
	assert.False(t, found, "a route still reaches the person who was erased")

	// Every session of theirs, not only the one that asked.
	assert.False(t, h.sessions.validate(session))
	assert.False(t, h.sessions.validate(other))

	// Every token they minted stops speaking, and stops naming them.
	_, live := tokens.Lookup(sha256Hex(raw))
	assert.False(t, live, "a token minted by an erased person still authenticates")
	listed, err := tokens.List()
	require.NoError(t, err)
	require.Len(t, listed, 1)
	assert.Empty(t, listed[0].MintedByUser)
	assert.Empty(t, listed[0].MintedByDisplayName)
	assert.Empty(t, listed[0].MintedBy)

	// The passkey, and only theirs.
	theirs, err := h.creds.doorCredentials("garden")
	require.NoError(t, err)
	assert.Empty(t, theirs)
	owners, err := h.creds.doorCredentials(NamespaceDefault)
	require.NoError(t, err)
	assert.Len(t, owners, 1, "somebody else's device was forgotten too")

	// The store cannot take the identity lines back, so the node says the
	// erasure happened rather than pretending they were never there.
	assert.Contains(t, kept.predicates(), PredicateErased)
	for _, as := range kept.wrote {
		if as.Predicates[0] != PredicateErased {
			continue
		}
		assert.Equal(t, erasedUserID, as.Subjects[0])
		assert.NotContains(t, as.Attributes, "display_name")
		assert.NotContains(t, as.Attributes, "handle")
		assert.NotContains(t, as.Attributes, "route")
	}
}

// The session that erased itself is over, and the cookie goes with it.
func TestTheSessionThatErasedItselfIsOver(t *testing.T) {
	h, _, _ := erasingNode(t)
	session := sessionFor(t, h, erasedRoute)

	rec := eraseSelf(h, session)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.False(t, h.sessions.validate(session))

	cleared := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName && c.MaxAge < 0 {
			cleared = true
		}
	}
	assert.True(t, cleared, "the browser was left holding the session it just erased")
}

// ROOT erases anyone, by id. A person who cannot reach the node any more still
// has the right, and somebody has to be able to act on it for them.
func TestRootErasesAnotherPersonByID(t *testing.T) {
	h, users, _ := erasingNode(t)
	root := sessionFor(t, h, mastodonAccount)

	rec := eraseByID(h, root, erasedUserID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	_, found, err := users.ByRoute(erasedRoute)
	require.NoError(t, err)
	assert.False(t, found)

	// ROOT is still ROOT afterwards.
	assert.True(t, h.sessions.validate(root))
}

// Somebody who walked up to a door may erase themselves and nobody else. The
// right is the data subject's own; it is not a way to reach other people.
func TestAPublicRegistrationCannotEraseAnotherPerson(t *testing.T) {
	h, users, _ := erasingNode(t)
	session := sessionFor(t, h, erasedRoute)

	rec := eraseByID(h, session, "US-USER-THEOWNER")
	assert.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())

	_, found, err := users.ByRoute(mastodonAccount)
	require.NoError(t, err)
	assert.True(t, found, "a public registration erased somebody else")
}

// A token speaks for a person, and a stolen one must not be able to delete
// them. Erasing is something the person at the browser does.
func TestATokenCannotErase(t *testing.T) {
	h, users, tokens := erasingNode(t)

	raw, _, err := tokens.Create(NewToken{
		Label: "ci", MintedBy: mastodonAccount,
		MintedByUser: "US-USER-THEOWNER", Level: LevelSuper,
	})
	require.NoError(t, err)

	for _, path := range []string{"/auth/user", "/auth/users/" + erasedUserID} {
		req := httptest.NewRequest(http.MethodDelete, path, nil)
		req.Header.Set("Authorization", "Bearer "+raw)
		rec := httptest.NewRecorder()
		if path == "/auth/user" {
			h.sessionOnly(h.handleEraseSelf)(rec, req)
		} else {
			h.sessionOnly(h.handleEraseByID)(rec, req)
		}
		assert.Equal(t, http.StatusUnauthorized, rec.Code, path)
	}

	_, found, err := users.ByRoute(erasedRoute)
	require.NoError(t, err)
	assert.True(t, found, "a bearer token erased a person")
}

// A node whose owner erased themselves has no owner. That is node:claimed's
// inverse, and it is not this act — so the route comes out of am.toml first.
func TestRootCannotEraseItselfWhileItsRouteIsListed(t *testing.T) {
	h, users, _ := erasingNode(t)
	root := sessionFor(t, h, mastodonAccount)

	rec := eraseSelf(h, root)
	require.Equal(t, http.StatusConflict, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), mastodonAccount)
	assert.Contains(t, rec.Body.String(), "root_identities")

	_, found, err := users.ByRoute(mastodonAccount)
	require.NoError(t, err)
	assert.True(t, found)

	// And not by id either — the refusal is about the User, not the path.
	assert.Equal(t, http.StatusConflict, eraseByID(h, root, "US-USER-THEOWNER").Code)
}

// A token outlives the session that minted it, so erasing the person has to
// reach it. Presenting it after is presenting nothing.
func TestATokenMintedByAnErasedPersonNoLongerAdmits(t *testing.T) {
	h, _, tokens := erasingNode(t)

	raw, _, err := tokens.Create(NewToken{
		Label: "ana's cron", MintedBy: erasedRoute,
		MintedByUser: erasedUserID, MintedByDisplayName: "ana", Level: LevelAttestor,
	})
	require.NoError(t, err)

	admitted := h.Middleware(everyLevel, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	before := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	before.Header.Set("Authorization", "Bearer "+raw)
	rec := httptest.NewRecorder()
	admitted(rec, before)
	require.Equal(t, http.StatusOK, rec.Code, "the token did not work before the erasure")

	require.Equal(t, http.StatusOK, eraseSelf(h, sessionFor(t, h, erasedRoute)).Code)

	after := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	after.Header.Set("Authorization", "Bearer "+raw)
	rec = httptest.NewRecorder()
	admitted(rec, after)
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "a token still speaks for somebody who was erased")
}

// Erasure is complete where completeness is possible and says so where it is
// not. What is left, and why, is the answer rather than a claim nobody checked.
func TestTheAnswerSaysWhatWasErasedAndWhatRemains(t *testing.T) {
	h, _, tokens := erasingNode(t)
	_, _, err := tokens.Create(NewToken{
		Label: "ana's cron", MintedBy: erasedRoute,
		MintedByUser: erasedUserID, MintedByDisplayName: "ana", Level: LevelAttestor,
	})
	require.NoError(t, err)

	rec := eraseSelf(h, sessionFor(t, h, erasedRoute))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	said := erasureOf(t, rec)
	assert.Equal(t, erasedUserID, said.User)
	assert.True(t, said.Gone.UserRecord)
	assert.Equal(t, 1, said.Gone.Sessions)
	assert.Equal(t, 1, said.Gone.AccessTokens)
	assert.Equal(t, 1, said.Gone.PasskeyCredentials)

	require.NotEmpty(t, said.Remains, "an erasure that claims to reach everything is claiming too much")
	for _, left := range said.Remains {
		assert.NotEmpty(t, left.What)
		assert.NotEmpty(t, left.Where)
		assert.NotEmpty(t, left.Why)
	}

	// The answer says what is left; it does not say the person.
	assert.NotContains(t, rec.Body.String(), "ana@example.com")
	assert.NotContains(t, rec.Body.String(), erasedRoute)
}

// Nothing to erase is not an erasure. An id no User holds says so rather than
// answering as though somebody was forgotten.
func TestErasingAnIDNobodyHoldsIsNotAnErasure(t *testing.T) {
	h, _, _ := erasingNode(t)
	root := sessionFor(t, h, mastodonAccount)

	rec := eraseByID(h, root, "US-USER-NOBODY")
	assert.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

// A deployment that keeps no Users has nobody to erase, and says that rather
// than answering as though it did.
func TestANodeWithNoUserStoreCannotErase(t *testing.T) {
	h := &Handler{sessions: newSessionStore(24), logger: testLogger()}
	h.SetIdentities([]string{mastodonAccount}, nil)
	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	assert.Equal(t, http.StatusServiceUnavailable, eraseSelf(h, session).Code)
}
