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

// Unset, a ceremony reaches this machine. It does not reach wherever the
// caller said, which is what the header version handed away.
func TestAnUnsetOriginIsLoopback(t *testing.T) {
	h := &Handler{loopbackOrigin: "http://127.0.0.1:8770"}

	assert.Equal(t, "http://127.0.0.1:8770", h.publicOrigin())
}

// The redirect_uri decides where a provider delivers an authorization code.
// Host and X-Forwarded-Host are written by whoever is talking to us.
func TestNoHeaderReachesTheRedirectURI(t *testing.T) {
	h := &Handler{loopbackOrigin: "http://127.0.0.1:8770"}

	spoofed := httptest.NewRequest(http.MethodGet, "http://backend:8080/auth/binding/start", nil)
	spoofed.Header.Set("X-Forwarded-Proto", "https")
	spoofed.Header.Set("X-Forwarded-Host", "attacker.example")
	spoofed.Host = "attacker.example"

	origin := h.publicOrigin()
	assert.Equal(t, "http://127.0.0.1:8770", origin)
	assert.NotContains(t, origin, "attacker.example")

	h.SetPublicOrigin("https://api.q.example.com")
	assert.Equal(t, "https://api.q.example.com", h.publicOrigin())
}

// The page and the API can be different hosts, so the configured origin is
// used verbatim rather than checked against where the page is.
func TestAConfiguredOriginIsTakenAsGiven(t *testing.T) {
	h := &Handler{}
	h.SetPublicOrigin("  https://api.q.example.com/  ")

	assert.Equal(t, "https://api.q.example.com", h.publicOrigin())
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
