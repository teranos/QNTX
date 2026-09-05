package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An app is a door. Its scheme stands in the door's origins beside the site,
// and a ceremony run in Safari is sent back through it. No browser presents a
// passkey for a scheme, so the relying party never hears of it.
//
// "security is a server concern"

func gardenWithAnApp() Door {
	d := garden()
	d.Origins = append(d.Origins, "garden://door")
	return d
}

// The scheme is a place the node may send somebody: am.toml named it.
func TestAnAppsSchemeIsSomewhereToReturn(t *testing.T) {
	h := handlerWithDoors(t, gardenWithAnApp())

	req := httptest.NewRequest(http.MethodGet, "/auth/binding/go", nil)
	req.Header.Set("Referer", "garden://door/")

	assert.Equal(t, "garden://door", h.returnableTo(req))
}

// The scheme reaches the door's namespace, the way the site does.
func TestAnAppsSchemeReachesItsDoorsNamespace(t *testing.T) {
	h := handlerWithDoors(t, gardenWithAnApp())

	opened, ok := h.doors.at("garden://door")
	require.True(t, ok)
	assert.Equal(t, "garden", opened.namespace)
}

// The relying party is told the site's origins and not the scheme. A scheme
// there would be an origin the rp id cannot cover, which every door refused.
func TestAnAppsSchemeIsNotAPasskeyOrigin(t *testing.T) {
	h := handlerWithDoors(t, gardenWithAnApp())

	opened, ok := h.doors.at("https://portal.garden.test")
	require.True(t, ok)
	assert.ElementsMatch(t, []string{"https://portal.garden.test", "https://app.garden.test"},
		opened.rp.Config.RPOrigins)
}

// The node's own door is rp_origins, and an app onto default stands there.
func TestAnAppOntoDefaultStandsInRPOrigins(t *testing.T) {
	h, err := New(
		nil, "q.sbvh.nl", []string{"https://q.sbvh.nl", "qntx://door"},
		8770, 8820, 24, testLogger(),
		func(next http.HandlerFunc) http.HandlerFunc { return next },
		nil, nil, false, []string{mastodonAccount}, nil,
	)
	require.NoError(t, err)

	opened, ok := h.doors.at("qntx://door")
	require.True(t, ok)
	assert.Equal(t, NamespaceDefault, opened.namespace)
	assert.Equal(t, []string{"https://q.sbvh.nl"}, h.webauthn.Config.RPOrigins)
}

// A page at a scheme sends no Referer, so the navigation names its door. The
// node accepts the name only when am.toml already did: a stranger naming a
// door they do not stand at is sent nowhere, the same as a stranger's Referer.
func TestANavigationMayNameItsDoor(t *testing.T) {
	h := handlerWithDoors(t, gardenWithAnApp())

	named := httptest.NewRequest(http.MethodGet, "/auth/binding/go?door=garden%3A%2F%2Fdoor", nil)
	assert.Equal(t, "garden://door", h.returnableTo(named))

	stranger := httptest.NewRequest(http.MethodGet, "/auth/binding/go?door=evil%3A%2F%2Fdoor", nil)
	assert.Empty(t, h.returnableTo(stranger))
}

// The door named on the request is also the door the ceremony consents under.
func TestANamedDoorPicksItsOwnClient(t *testing.T) {
	h := twoDoors(t)
	h.nodeKey = testNodeKey(t)

	req := httptest.NewRequest(http.MethodGet,
		"/auth/binding/go?provider=google&door=https%3A%2F%2Fgarden.example&peer_pubkey_hex="+testPeerPubkeyHex, nil)

	recorded := httptest.NewRecorder()
	h.handleBindingGo(recorded, req)

	require.Equal(t, http.StatusFound, recorded.Code)
	assert.Contains(t, recorded.Header().Get("Location"), "gardens-own-client")
}
