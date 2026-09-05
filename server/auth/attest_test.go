package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/ats/types"
	"go.uber.org/zap"
)

// An attestor that keeps what it was handed, so a test can ask what the door
// wrote down rather than what it logged.
type memAttestor struct{ wrote []*types.As }

func (m *memAttestor) CreateAttestation(as *types.As) error {
	m.wrote = append(m.wrote, as)
	return nil
}

func (m *memAttestor) predicates() []string {
	said := make([]string, 0, len(m.wrote))
	for _, as := range m.wrote {
		said = append(said, as.Predicates...)
	}
	return said
}

// A display_name settles once and can never be taken back, so when it settled
// is a fact the owner can go and look at.
func TestSettlingANameIsAttested(t *testing.T) {
	h, _, session := arrivingHandler(t)
	kept := &memAttestor{}
	h.SetAttestor(kept)

	require.Equal(t, http.StatusOK, arrive(h, session, `{"display_name":"tim"}`).Code)

	assert.Contains(t, kept.predicates(), PredicateNamed)
}

// Saying nothing settles nothing, so there is nothing to record.
func TestArrivingWithNoNameIsNotAttested(t *testing.T) {
	h, _, session := arrivingHandler(t)
	kept := &memAttestor{}
	h.SetAttestor(kept)

	require.Equal(t, http.StatusOK, arrive(h, session, `{}`).Code)

	assert.NotContains(t, kept.predicates(), PredicateNamed)
}

// A token outlives the session that minted it, so both ends of its life are a
// record rather than a log line that rotates away.
func TestATokensLifeIsAttested(t *testing.T) {
	h, store := grantHandler(t)
	h.SetIdentities([]string{mastodonAccount}, nil)
	kept := &memAttestor{}
	h.SetAttestor(kept)

	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	mint(h, rec, mintRequest(`{"label":"mbp","level":"ATTESTOR","scope":{"read":["*"],"write":["*"]}}`, session))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	listed, err := store.List()
	require.NoError(t, err)
	require.Len(t, listed, 1)

	del := httptest.NewRequest(http.MethodDelete, "/auth/tokens/"+listed[0].ID, nil)
	del.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	byID(h, httptest.NewRecorder(), del)

	assert.Contains(t, kept.predicates(), PredicateMinted)
	assert.Contains(t, kept.predicates(), PredicateRevoked)
}

// A store that did not answer is a fact about the deployment. It lived only in
// a log, which is where the S3 outage hid for ninety minutes.
func TestAStoreThatDidNotAnswerIsAttested(t *testing.T) {
	kept := &memAttestor{}
	h := &Handler{
		users:    brokenUsers{},
		sessions: newSessionStore(24),
		logger:   zap.NewNop().Sugar(),
	}
	h.SetAttestor(kept)
	h.SetIdentities([]string{mastodonAccount}, nil)

	session, err := h.sessions.create(mastodonAccount, User{})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/auth/user/arrive",
		strings.NewReader(`{"display_name":"tim"}`))
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
	rec := httptest.NewRecorder()
	h.HandleArrive(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, kept.predicates(), PredicateUnanswered)
}

// A node acquires an owner once and cannot acquire another. The moment it stops
// being claimable is the one worth going back to, and it lived only in a log.
func TestClaimingTheNodeIsAttested(t *testing.T) {
	h, _ := handlerWithUsers(t)
	kept := &memAttestor{}
	h.SetAttestor(kept)

	u, err := h.joinUser(mastodonAccount, mastodonBinding("@tim@mastodon.example"), "did:key:zBrowser")
	require.NoError(t, err)

	require.Contains(t, kept.predicates(), PredicateClaimed)
	for _, as := range kept.wrote {
		if as.Predicates[0] != PredicateClaimed {
			continue
		}
		assert.Equal(t, u.ID, as.Subjects[0], "the claim names somebody other than the User it minted")
		assert.Equal(t, mastodonAccount, as.Attributes["route"])
		assert.Equal(t, string(LevelRoot), as.Attributes["level"])
	}
}

// A second listed route joins the ROOT User that already exists. The node had an
// owner before that route proved anything, so nothing was claimed.
func TestASecondRouteDoesNotClaimTheNodeAgain(t *testing.T) {
	h, _ := handlerWithUsers(t)

	_, err := h.joinUser(mastodonAccount, mastodonBinding("@tim@mastodon.example"), "did:key:zBrowser")
	require.NoError(t, err)

	kept := &memAttestor{}
	h.SetAttestor(kept)

	_, err = h.joinUser(atprotoAccount,
		accountBinding("atproto", atprotoAccount, "@tim.bsky.social"), "did:key:zBrowser")
	require.NoError(t, err)

	assert.NotContains(t, kept.predicates(), PredicateClaimed)
}
