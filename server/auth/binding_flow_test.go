package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A callback is a URL the provider can be made to visit again. Consuming the
// ceremony is what stops a replayed redirect producing a second binding.
func TestACeremonyIsSpentOnce(t *testing.T) {
	var flows bindingFlows
	state, err := flows.open(flow{provider: "mastodon", peerPubkeyHex: "abcd"})
	require.NoError(t, err)

	first, ok := flows.close(state)
	require.True(t, ok)
	assert.Equal(t, "abcd", first.peerPubkeyHex)

	_, ok = flows.close(state)
	assert.False(t, ok)
}

// An abandoned ceremony is a client secret sitting in memory, so it expires
// rather than waiting for a callback that is not coming.
func TestAnOldCeremonyIsNoCeremony(t *testing.T) {
	var flows bindingFlows
	state, err := flows.open(flow{provider: "mastodon"})
	require.NoError(t, err)

	flows.pending.Store(state, flow{
		provider:  "mastodon",
		startedAt: time.Now().Add(-bindingFlowTTL - time.Second),
	})

	_, ok := flows.close(state)
	assert.False(t, ok)
}

// Two ceremonies must not collide, or one person's callback finishes another
// person's link.
func TestCeremoniesDoNotShareAState(t *testing.T) {
	var flows bindingFlows
	first, err := flows.open(flow{provider: "mastodon"})
	require.NoError(t, err)
	second, err := flows.open(flow{provider: "atproto"})
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
}

// The redirect URI is registered with the provider and must match on return,
// so it is read off the request the browser actually made.
func TestPublicOriginFollowsTheProxy(t *testing.T) {
	h := &Handler{}

	plain := httptest.NewRequest(http.MethodGet, "http://localhost:8080/auth/binding/start", nil)
	assert.Equal(t, "http://localhost:8080", h.publicOrigin(plain))

	proxied := httptest.NewRequest(http.MethodGet, "http://backend:8080/auth/binding/start", nil)
	proxied.Header.Set("X-Forwarded-Proto", "https")
	proxied.Header.Set("X-Forwarded-Host", "q.example.com")
	assert.Equal(t, "https://q.example.com", h.publicOrigin(proxied))
}

// A deployment behind TLS with no proxy headers still has to build an https
// redirect, or the provider is registered against a URL nobody can reach.
func TestPublicOriginIsHTTPSWhenDeployed(t *testing.T) {
	h := &Handler{secureCookies: true}
	req := httptest.NewRequest(http.MethodGet, "http://q.example.com/auth/binding/start", nil)

	assert.Equal(t, "https://q.example.com", h.publicOrigin(req))
}

func TestPeerPubkeyMustBeAKey(t *testing.T) {
	_, err := decodePeerPubkey("")
	assert.Error(t, err)

	_, err = decodePeerPubkey("not-hex")
	assert.Error(t, err)

	_, err = decodePeerPubkey("abcd")
	assert.Error(t, err)

	_, err = decodePeerPubkey("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	assert.NoError(t, err)
}
