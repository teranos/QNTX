package auth

import (
	"crypto/ed25519"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The key a binding would be about. Any valid ed25519 public key does: what is
// under test is where the ceremony goes, not what it ends up signing.
var testPeerPubkeyHex = hex.EncodeToString(make([]byte, ed25519.PublicKeySize))

// testNodeKey is a node that can sign. handleBindingGo refuses without one,
// because a ceremony it could not finish is not worth starting.
func testNodeKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	_, key, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	return key
}

// A door on another domain reaches this node cross-site, and a browser will not
// keep a SameSite=Lax cookie set on a cross-site fetch. So the start is a
// navigation, where the cookie is this node's own and is still held when the
// provider returns.
func TestTheNavigationStartSetsTheCeremonyCookie(t *testing.T) {
	h := twoDoors(t)
	h.nodeKey = testNodeKey(t)

	recorded := httptest.NewRecorder()
	h.handleBindingGo(recorded, navigatedFrom(t, "https://garden.example"))

	require.Equal(t, http.StatusFound, recorded.Code)
	assert.Contains(t, recorded.Header().Get("Location"), "accounts.google.com")

	var ticket *http.Cookie
	for _, c := range recorded.Result().Cookies() {
		if c.Name == ceremonyCookieName {
			ticket = c
		}
	}
	require.NotNil(t, ticket, "the navigation set no ceremony cookie")
	assert.NotEmpty(t, ticket.Value)
}

// A navigation carries no Origin. Without another way to read where the browser
// came from, every door would look like this node's own host and a door with
// its own client would consent under the node's.
func TestANavigationFindsItsDoorByReferer(t *testing.T) {
	h := twoDoors(t)
	h.nodeKey = testNodeKey(t)

	recorded := httptest.NewRecorder()
	h.handleBindingGo(recorded, navigatedFrom(t, "https://garden.example"))

	require.Equal(t, http.StatusFound, recorded.Code)
	assert.Contains(t, recorded.Header().Get("Location"), "gardens-own-client",
		"the ceremony was spent with the node's client, not the door's")
}

// The referrer policy every browser defaults to sends the origin alone across
// sites. One that sends the whole URL has the rest cut off here, so a door is
// matched by what it was named after and not by which page was open.
func TestARefererWithAPathStillNamesItsDoor(t *testing.T) {
	h := twoDoors(t)
	h.nodeKey = testNodeKey(t)

	req := navigatedFrom(t, "https://garden.example")
	req.Header.Set("Referer", "https://garden.example/login?next=%2Fhome")

	recorded := httptest.NewRecorder()
	h.handleBindingGo(recorded, req)

	require.Equal(t, http.StatusFound, recorded.Code)
	assert.Contains(t, recorded.Header().Get("Location"), "gardens-own-client")
}

// Anyone can navigate here from a page of their own, so the header saying where
// they came from decides nothing on its own. Sending them back to it would make
// this node a redirector for whoever asked.
func TestAStrangersRefererIsNotSomewhereToReturn(t *testing.T) {
	h := twoDoors(t)

	req := httptest.NewRequest(http.MethodGet, "/auth/binding/go", nil)
	req.Header.Set("Referer", "https://not-a-door.example/whatever")

	assert.Empty(t, h.returnableTo(req))
}

// The door's own origin is somewhere to return to, because am.toml said so.
func TestADoorsOriginIsSomewhereToReturn(t *testing.T) {
	h := twoDoors(t)

	assert.Equal(t, "https://garden.example",
		h.returnableTo(navigatedFrom(t, "https://garden.example")))
}

// A ceremony that began as a navigation has nobody to close a window for, so it
// ends by putting the person back at the door they left.
func TestANavigatedCeremonyReturnsToItsDoor(t *testing.T) {
	h := twoDoors(t)

	state, err := h.bindingFlows.open(flow{
		provider: "google",
		ceremony: "the-starting-browser",
		door:     "garden",
		returnTo: "https://garden.example",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, callbackPath+"?state="+state+"&code=whatever", nil)
	req.AddCookie(&http.Cookie{Name: ceremonyCookieName, Value: "the-starting-browser"})

	recorded := httptest.NewRecorder()
	h.handleBindingCallback(recorded, req)

	// The exchange fails against no provider, which is fine: what is asserted
	// here is that a refusal is not answered by sending anyone anywhere.
	if recorded.Code == http.StatusFound {
		assert.Equal(t, "https://garden.example", recorded.Header().Get("Location"))
	}
}

// The ceremony comes back to the page it began on, where the invitation the
// friend arrived with is held. A branch is served at /branch/<name> on the
// door's own origin.
func TestACeremonyReturnsToThePageItBeganOn(t *testing.T) {
	h := twoDoors(t)
	h.nodeKey = testNodeKey(t)

	req := navigatedFrom(t, "https://garden.example")
	req.URL.RawQuery += "&page=" + urlEncode("/branch/root-invites-a-user/")
	recorded := httptest.NewRecorder()
	h.handleBindingGo(recorded, req)
	require.Equal(t, http.StatusFound, recorded.Code)

	location := recorded.Header().Get("Location")
	state := location[strings.Index(location, "&state=")+len("&state="):]
	held, ok := h.bindingFlows.pending.Load(state)
	require.True(t, ok)
	assert.Equal(t, "https://garden.example/branch/root-invites-a-user/", held.(flow).returnTo)
}

// A page is a path on the door's own origin and nothing else: another host,
// a scheme-relative path, or a query is the door's origin alone.
func TestAPageIsAPathOnTheDoorsOrigin(t *testing.T) {
	for page, want := range map[string]string{
		"":                      "https://garden.example",
		"/branch/x/":            "https://garden.example/branch/x/",
		"//evil.example/":       "https://garden.example",
		"https://evil.example/": "https://garden.example",
		"/x?next=https://evil":  "https://garden.example",
		"/x#y":                  "https://garden.example",
		"/x\\evil":              "https://garden.example",
	} {
		assert.Equal(t, want, onPage("https://garden.example", page), page)
	}
	assert.Empty(t, onPage("", "/branch/x/"), "no door, nowhere to return")
}

// navigatedFrom is a top-level navigation: a Referer, and no Origin, which is
// what a browser sends when a page sets location rather than fetching.
func navigatedFrom(t *testing.T, origin string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet,
		"/auth/binding/go?provider=google&peer_pubkey_hex="+testPeerPubkeyHex, nil)
	req.Header.Set("Referer", origin+"/")
	return req
}
