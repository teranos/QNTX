package auth

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The tests point providers at httptest, which listens on loopback. guardDial
// is exercised directly below, so clearing the wiring here does not leave the
// rule untested.
func init() { providerDialControl = nil }

// A provider is somewhere on the internet. Every address below is somewhere
// only this node can reach, and reaching it for a caller is that caller
// reading our network through us.
func TestGuardDialRefusesWhatTheInternetCannotRoute(t *testing.T) {
	refused := []string{
		"127.0.0.1:443",
		"[::1]:443",
		"10.1.2.3:443",
		"192.168.1.1:443",
		"172.16.0.1:443",
		"169.254.169.254:80",
		"[fd00::1]:443",
		"[fe80::1]:443",
		"0.0.0.0:443",
		"100.64.0.1:443",
		"224.0.0.1:443",
		"255.255.255.255:443",
	}
	for _, address := range refused {
		assert.Error(t, guardDial("tcp", address, nil), address)
	}
}

func TestGuardDialAllowsAPublicAddress(t *testing.T) {
	for _, address := range []string{"1.1.1.1:443", "[2606:4700:4700::1111]:443"} {
		assert.NoError(t, guardDial("tcp", address, nil), address)
	}
}

// The hostname check happens once; DNS answers again at dial time. A name that
// resolves to loopback is refused at the address, not at the name.
func TestGuardDialIsAboutTheAddressNotTheName(t *testing.T) {
	err := guardDial("tcp", "127.0.0.1:8080", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "127.0.0.1")
}

func TestIsPublicIPMatchesGuardDial(t *testing.T) {
	assert.False(t, isPublicIP(net.ParseIP("127.0.0.1")))
	assert.False(t, isPublicIP(net.ParseIP("::1")))
	assert.False(t, isPublicIP(net.ParseIP("192.168.0.1")))
	assert.True(t, isPublicIP(net.ParseIP("8.8.8.8")))
	assert.True(t, isPublicIP(net.ParseIP("2001:4860:4860::8888")))
}

// The caller types the host, so a single-label name or a literal internal
// address is refused before anything is contacted.
func TestNormalizeHostRefusesWhatIsNotOnTheInternet(t *testing.T) {
	for _, typed := range []string{"localhost", "metadata", "internal", "127.0.0.1", "10.0.0.1", "169.254.169.254"} {
		_, err := normalizeHost(typed)
		assert.Error(t, err, typed)
	}
}

func TestNormalizeHostStillTakesARealInstance(t *testing.T) {
	for _, typed := range []string{"mastodon.social", "bsky.social", "shiitake.us-east.host.bsky.network"} {
		_, err := normalizeHost(typed)
		assert.NoError(t, err, typed)
	}
}
