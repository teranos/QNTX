package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	qntxtest "github.com/teranos/QNTX/internal/testing"
)

// "i wish i was able to assume the identity of another user, one that isnt ROOT, but also be able to go back."

// becomingNode is ROOT with a session, and another User who walked up to the
// door onto the-app with a session of their own. The node keeps tokens and
// writes down what it attests.
type becomingNode struct {
	h            *Handler
	users        *memUsers
	kept         *memAttestor
	rootSession  string
	other        User
	otherSession string
}

func aNodeToBecomeOn(t *testing.T) becomingNode {
	t.Helper()
	h, store, rootSession := switchingHandler(t)
	other := User{
		ID: "US-OTHER-1", DisplayName: "other", Level: LevelPublicRegistration, Namespace: "the-app",
		Keys: []UserKey{{DID: "did:key:zOther", Origin: OriginBrowser}}, CreatedAt: 2,
	}
	require.NoError(t, store.Put(other))
	otherSession, err := h.sessions.create("did:key:zOther", other)
	require.NoError(t, err)
	kept := &memAttestor{}
	h.SetAttestor(kept)
	tokens, _, err := OpenTokenTable(qntxtest.CreateTestDB(t), &countingTokens{})
	require.NoError(t, err)
	h.tokens = tokens
	return becomingNode{h: h, users: store, kept: kept, rootSession: rootSession, other: other, otherSession: otherSession}
}

func (n becomingNode) become(session, id string) *httptest.ResponseRecorder {
	return asRoot(n.h, session, http.MethodPost, "/auth/users/"+id+"/become")
}

// admittedAs is who the gate lets a request through as, and what it answered.
func (n becomingNode) admittedAs(present func(*http.Request)) (Admission, int) {
	var seen Admission
	guarded := n.h.Middleware("/test", everyLevel, func(w http.ResponseWriter, r *http.Request) {
		seen, _ = AdmissionFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/attestations", nil)
	present(req)
	rec := httptest.NewRecorder()
	guarded(rec, req)
	return seen, rec.Code
}

func withSession(session string) func(*http.Request) {
	return func(r *http.Request) { r.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session}) }
}

func withBearer(raw string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+raw) }
}

// mintedFor writes down a token the other User minted, presented as raw.
func (n becomingNode) mintedFor(t *testing.T, raw, label string) {
	t.Helper()
	_, err := n.h.tokens.Issue(IssuedToken{
		Hash: sha256Hex(raw), DID: "did:key:z" + label, Label: label,
		MintedBy: "did:key:zOther", MintedByUser: n.other.ID, Level: LevelAttestor, Namespaces: []string{"the-app"},
		ExpiresAt: NeverEnds(),
	})
	require.NoError(t, err)
}

// "but its attested that they have been becoming and unbecoming"
// "until ROOT unbecomes that user and is itself again."
func TestRootBecomesAUserAndIsThemUntilItUnbecomesThem(t *testing.T) {
	n := aNodeToBecomeOn(t)

	rec := n.become(n.rootSession, n.other.ID)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	is, code := n.admittedAs(withSession(n.rootSession))
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, n.other.ID, is.UserID)
	assert.Equal(t, string(LevelPublicRegistration), is.LevelName())
	assert.Equal(t, []string{"the-app"}, is.Namespaces)

	rec = flip(n.h, n.rootSession, "unbecome")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	is, code = n.admittedAs(withSession(n.rootSession))
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, string(LevelRoot), is.LevelName())
	assert.Equal(t, n.users.held[0].ID, is.UserID)

	assert.Subset(t, n.kept.predicates(), []string{PredicateBecame, PredicateUnbecame})
}

// "So, yes, this can be destructive in the sense that a session may get revoked."
func TestBecomingAUserEndsTheirSessions(t *testing.T) {
	n := aNodeToBecomeOn(t)
	_, code := n.admittedAs(withSession(n.otherSession))
	require.Equal(t, http.StatusOK, code)

	require.Equal(t, http.StatusOK, n.become(n.rootSession, n.other.ID).Code)
	_, code = n.admittedAs(withSession(n.otherSession))
	assert.Equal(t, http.StatusUnauthorized, code)

	// A session they make while ROOT is them is them, and they are not themselves.
	again, err := n.h.sessions.create("did:key:zOther", n.other)
	require.NoError(t, err)
	_, code = n.admittedAs(withSession(again))
	assert.Equal(t, http.StatusForbidden, code)
}

// "But for tokens its tmp access loss."
func TestTheirTokensAreRefusedWhileRootIsThemAndWorkAfter(t *testing.T) {
	n := aNodeToBecomeOn(t)
	n.mintedFor(t, "raw-their-own", "their-own")
	_, code := n.admittedAs(withBearer("raw-their-own"))
	require.Equal(t, http.StatusOK, code)

	require.Equal(t, http.StatusOK, n.become(n.rootSession, n.other.ID).Code)
	_, code = n.admittedAs(withBearer("raw-their-own"))
	assert.Equal(t, http.StatusForbidden, code)

	require.Equal(t, http.StatusOK, flip(n.h, n.rootSession, "unbecome").Code)
	_, code = n.admittedAs(withBearer("raw-their-own"))
	assert.Equal(t, http.StatusOK, code)
}

// A token minted while ROOT is them lives as long as the becoming.
func TestATokenMintedWhileRootIsThemLivesAsLongAsTheBecoming(t *testing.T) {
	n := aNodeToBecomeOn(t)
	require.Equal(t, http.StatusOK, n.become(n.rootSession, n.other.ID).Code)
	n.mintedFor(t, "raw-while-become", "while-become")
	_, code := n.admittedAs(withBearer("raw-while-become"))
	require.Equal(t, http.StatusOK, code)

	require.Equal(t, http.StatusOK, flip(n.h, n.rootSession, "unbecome").Code)
	_, code = n.admittedAs(withBearer("raw-while-become"))
	assert.Equal(t, http.StatusUnauthorized, code)
}

// ROOT becomes nobody who is ROOT, only ROOT becomes anybody, and one User is
// become by one becoming at a time.
func TestWhoMayBecomeWhom(t *testing.T) {
	n := aNodeToBecomeOn(t)
	root := n.users.held[0]

	assert.Equal(t, http.StatusBadRequest, n.become(n.rootSession, root.ID).Code)
	assert.Equal(t, http.StatusForbidden, n.become(n.otherSession, root.ID).Code)
	assert.Equal(t, http.StatusNotFound, n.become(n.rootSession, "US-NOBODY-1").Code)

	require.Equal(t, http.StatusOK, n.become(n.rootSession, n.other.ID).Code)
	second, err := n.h.sessions.create(mastodonAccount, root)
	require.NoError(t, err)
	assert.Equal(t, http.StatusConflict, n.become(second, n.other.ID).Code)
}

// A restart is a new handler on the same db: ROOT is still them, and they are
// still not themselves.
func TestABecomingSurvivesARestart(t *testing.T) {
	n := aNodeToBecomeOn(t)
	db := qntxtest.CreateTestDB(t)
	n.h.sessions = newSessionStore(24).kept(db, zap.NewNop().Sugar())
	require.NoError(t, n.h.becomings.kept(db, zap.NewNop().Sugar()))
	rootSession, err := n.h.sessions.create(mastodonAccount, n.users.held[0])
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, n.become(rootSession, n.other.ID).Code)

	n.h.sessions = newSessionStore(24).kept(db, zap.NewNop().Sugar())
	n.h.becomings = becomings{}
	require.NoError(t, n.h.becomings.kept(db, zap.NewNop().Sugar()))

	is, code := n.admittedAs(withSession(rootSession))
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, n.other.ID, is.UserID)
	again, err := n.h.sessions.create("did:key:zOther", n.other)
	require.NoError(t, err)
	_, code = n.admittedAs(withSession(again))
	assert.Equal(t, http.StatusForbidden, code)
}

// ROOT's session ending is ROOT no longer being them: nobody would be left to
// unbecome them.
func TestABecomingEndsWithRootsSession(t *testing.T) {
	n := aNodeToBecomeOn(t)
	require.Equal(t, http.StatusOK, n.become(n.rootSession, n.other.ID).Code)
	n.h.sessions.invalidate(n.rootSession)
	n.h.unbecomeTheEnded()

	again, err := n.h.sessions.create("did:key:zOther", n.other)
	require.NoError(t, err)
	_, code := n.admittedAs(withSession(again))
	assert.Equal(t, http.StatusOK, code)
}

// A session that is not being anybody has nobody to unbecome.
func TestOnlyABecomingUnbecomes(t *testing.T) {
	n := aNodeToBecomeOn(t)
	assert.Equal(t, http.StatusConflict, flip(n.h, n.rootSession, "unbecome").Code)
}

// The i element asks who is looking, and is told that ROOT is being them.
func TestTheUserRootIsBeingSaysSo(t *testing.T) {
	n := aNodeToBecomeOn(t)
	require.Equal(t, http.StatusOK, n.become(n.rootSession, n.other.ID).Code)

	guarded := n.h.Middleware("/i/", everyLevel, n.h.HandleTheUser)
	req := httptest.NewRequest(http.MethodGet, "/i/", nil)
	withSession(n.rootSession)(req)
	rec := httptest.NewRecorder()
	guarded(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"user":"`+n.other.ID+`"`)
	assert.Contains(t, rec.Body.String(), `"becoming_by":"`+n.users.held[0].ID+`"`)
}
