package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// An app is a door. Its scheme stands in the door's origins beside the site,
// and a ceremony run in the browser is sent back through it. No browser presents a
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
		nil, &memUsers{}, false, []string{mastodonAccount}, nil,
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

// 「[Opus 5.5] theAppTakesBack is the ticket the app takes from the URL its sheet came back」
// 「[Opus 5.5] on, by the app's own rule (QNTX-Web ts/app-door.ts, ticketIn): the URL up to」
// 「[Opus 5.5] its query is exactly the door, and the query holds the ceremony. Empty where」
// 「[Opus 5.5] the app throws the URL away.」
func theAppTakesBack(door, cameBack string) string {
	cut := strings.Index(cameBack, "?")
	if cut < 0 || cameBack[:cut] != door {
		return ""
	}
	said, err := url.ParseQuery(cameBack[cut+1:])
	if err != nil {
		return ""
	}
	return said.Get("ceremony")
}

// "i cant login"
//
// 「[Opus 5.5] The app's sign-in, the whole way: it starts at the node naming its door and」
// 「[Opus 5.5] its page, GitHub answers, the sheet is sent back, the app takes the ticket」
// 「[Opus 5.5] back by its own rule, and the ticket collects the binding. With the page the」
// 「[Opus 5.5] app names, "/", the sheet came back to qntx://door/ and the app threw it」
// 「[Opus 5.5] away: every sign-in by a provider in the app ended there.」
func TestTheAppSignsInByAProviderTheWholeWay(t *testing.T) {
	standInGitHub(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "spent-once"})
		case "/user":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": 1739143, "login": "teranos"})
		}
	})
	for _, page := range []string{"/", "/branch/x/"} {
		h := handlerWithDoors(t, gardenWithAnApp())
		h.SetGitHubClient("the-nodes-client", "the-nodes-secret")
		h.nodeKey = testNodeKey(t)

		// 「[Opus 5.5] The app starts it, naming its door and the page it is on.」
		started := httptest.NewRecorder()
		h.handleBindingGo(started, httptest.NewRequest(http.MethodGet, "/auth/binding/go?provider=github&door=garden%3A%2F%2Fdoor&page="+
			urlEncode(page)+"&peer_pubkey_hex="+testPeerPubkeyHex, nil))
		require.Equal(t, http.StatusFound, started.Code, started.Body.String())
		toGitHub, err := url.Parse(started.Header().Get("Location"))
		require.NoError(t, err)
		var cookie *http.Cookie
		for _, c := range started.Result().Cookies() {
			if c.Name == ceremonyCookieName {
				cookie = c
			}
		}
		require.NotNil(t, cookie, page)

		// 「[Opus 5.5] GitHub sends the sheet back to the node, which holds the cookie it set.」
		back := httptest.NewRequest(http.MethodGet, callbackPath+"?state="+url.QueryEscape(toGitHub.Query().Get("state"))+"&code=the-code", nil)
		back.AddCookie(cookie)
		returned := httptest.NewRecorder()
		h.handleBindingCallback(returned, back)
		require.Equal(t, http.StatusFound, returned.Code, returned.Body.String())

		// 「[Opus 5.5] The app takes the ticket back from where the sheet landed.」
		cameBack := returned.Header().Get("Location")
		ticket := theAppTakesBack("garden://door", cameBack)
		require.NotEmpty(t, ticket, "the app throws away %s (page %s)", cameBack, page)

		// 「[Opus 5.5] And the ticket collects the binding, from the app's page, with no cookie.」
		collected := httptest.NewRecorder()
		h.handleBindingResult(collected, httptest.NewRequest(http.MethodGet, "/auth/binding/result?ceremony="+url.QueryEscape(ticket), nil))
		require.Equal(t, http.StatusOK, collected.Code, collected.Body.String())
		assert.Contains(t, collected.Body.String(), "github:1739143")
	}
}
