package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func ticketHandler() *Handler {
	return &Handler{logger: zap.NewNop().Sugar()}
}

// Linking happens before anyone can log in, so the ceremony cannot be gated on
// a session. The ticket is what makes the browser finishing a ceremony the
// browser that started it.
func TestACeremonyIsFinishedByTheBrowserThatStartedIt(t *testing.T) {
	h := ticketHandler()

	state, err := h.bindingFlows.open(flow{provider: "mastodon", ceremony: "the-starting-browser"})
	require.NoError(t, err)

	// A victim's browser follows a link someone else started. It carries no
	// ticket, because it never asked for one.
	victim := httptest.NewRequest(http.MethodGet,
		callbackPath+"?state="+state+"&code=whatever", nil)
	recorded := httptest.NewRecorder()
	h.handleBindingCallback(recorded, victim)

	assert.Equal(t, http.StatusForbidden, recorded.Code)
	assert.Contains(t, recorded.Body.String(), "started somewhere else")
}

func TestAWrongTicketIsNoTicket(t *testing.T) {
	h := ticketHandler()

	state, err := h.bindingFlows.open(flow{provider: "mastodon", ceremony: "the-starting-browser"})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, callbackPath+"?state="+state+"&code=whatever", nil)
	req.AddCookie(&http.Cookie{Name: ceremonyCookieName, Value: "some-other-browser"})
	recorded := httptest.NewRecorder()
	h.handleBindingCallback(recorded, req)

	assert.Equal(t, http.StatusForbidden, recorded.Code)
}

// The binding is collected by the ticket, not by the key it names. Anyone can
// read a peer pubkey off the wire; only the starting browser holds the ticket.
func TestABindingIsCollectedByTheTicket(t *testing.T) {
	h := ticketHandler()
	h.signedBindings.Store("the-ticket", heldBinding{
		binding:  SignedBinding{SignerPubkeyHex: "aa"},
		signedAt: time.Now(),
	})

	anonymous := httptest.NewRequest(http.MethodGet, "/auth/binding/result", nil)
	recorded := httptest.NewRecorder()
	h.handleBindingResult(recorded, anonymous)
	assert.Equal(t, http.StatusUnauthorized, recorded.Code)

	held := httptest.NewRequest(http.MethodGet, "/auth/binding/result", nil)
	held.AddCookie(&http.Cookie{Name: ceremonyCookieName, Value: "the-ticket"})
	recorded = httptest.NewRecorder()
	h.handleBindingResult(recorded, held)
	assert.Equal(t, http.StatusOK, recorded.Code)
	assert.Contains(t, recorded.Body.String(), "aa")
}

// A binding that answers a second poll is how the next ceremony silently
// returns the previous account.
func TestABindingIsCollectedOnce(t *testing.T) {
	h := ticketHandler()
	h.signedBindings.Store("the-ticket", heldBinding{signedAt: time.Now()})

	req := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/auth/binding/result", nil)
		r.AddCookie(&http.Cookie{Name: ceremonyCookieName, Value: "the-ticket"})
		return r
	}

	first := httptest.NewRecorder()
	h.handleBindingResult(first, req())
	assert.Equal(t, http.StatusOK, first.Code)

	second := httptest.NewRecorder()
	h.handleBindingResult(second, req())
	assert.Equal(t, http.StatusNotFound, second.Code)
}

func TestAnUncollectedBindingExpires(t *testing.T) {
	h := ticketHandler()
	h.signedBindings.Store("the-ticket", heldBinding{
		signedAt: time.Now().Add(-bindingFlowTTL - time.Second),
	})

	req := httptest.NewRequest(http.MethodGet, "/auth/binding/result", nil)
	req.AddCookie(&http.Cookie{Name: ceremonyCookieName, Value: "the-ticket"})
	recorded := httptest.NewRecorder()
	h.handleBindingResult(recorded, req)

	assert.Equal(t, http.StatusNotFound, recorded.Code)
}

// Starting a ceremony and asking for a challenge are both unauthenticated, so
// what bounds these maps is the sweep rather than anyone coming back.
func TestTheSweepDropsWhatNobodyCameBackFor(t *testing.T) {
	h := ticketHandler()

	fresh, err := h.bindingFlows.open(flow{provider: "mastodon"})
	require.NoError(t, err)
	stale, err := h.bindingFlows.open(flow{provider: "mastodon"})
	require.NoError(t, err)
	h.bindingFlows.pending.Store(stale, flow{
		provider:  "mastodon",
		startedAt: time.Now().Add(-bindingFlowTTL - time.Second),
	})

	h.signedBindings.Store("kept", heldBinding{signedAt: time.Now()})
	h.signedBindings.Store("dropped", heldBinding{
		signedAt: time.Now().Add(-bindingFlowTTL - time.Second),
	})

	live, err := h.layeChallenges.issue()
	require.NoError(t, err)
	h.layeChallenges.pending.Store("old", layeChallenge{
		issuedAt: time.Now().Add(-layeChallengeTTL - time.Second),
	})

	h.bindingFlows.sweep()
	h.sweepSignedBindings()
	h.layeChallenges.sweep()

	_, ok := h.bindingFlows.pending.Load(fresh)
	assert.True(t, ok)
	_, ok = h.bindingFlows.pending.Load(stale)
	assert.False(t, ok)

	_, ok = h.signedBindings.Load("kept")
	assert.True(t, ok)
	_, ok = h.signedBindings.Load("dropped")
	assert.False(t, ok)

	_, ok = h.layeChallenges.pending.Load(live)
	assert.True(t, ok)
	_, ok = h.layeChallenges.pending.Load("old")
	assert.False(t, ok)
}
