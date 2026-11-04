package auth

import (
	"context"
	"encoding/json"
	errors "github.com/teranos/sacred-error"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// "Given that I am ROOT and my google identity is the ROOT identity"

type connected struct{ token, project, by string }

// cloudHandler is a node whose Google login is ROOT's, and whose connector
// keeps what it was handed.
func cloudHandler(t *testing.T) (*Handler, *[]connected) {
	t.Helper()
	h := &Handler{users: &memUsers{}, sessions: newSessionStore(24), logger: zap.NewNop().Sugar()}
	h.SetIdentities([]string{"google:root-sub"}, nil)
	h.google = &OperatorClient{ID: "client-id", Secret: "client-secret"}
	var handed []connected
	h.SetGoogleCloud(func(_ context.Context, token, project, by string) (string, error) {
		handed = append(handed, connected{token, project, by})
		return "connected", nil
	})
	return h, &handed
}

// googleAnswering is Google's token and userinfo endpoints, answering as sub.
func googleAnswering(t *testing.T, sub string) {
	standInGoogle(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			require.NoError(t, r.ParseForm())
			assert.Equal(t, "the-code", r.PostForm.Get("code"))
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "cloud-token", "token_type": "Bearer"})
		case "/v1/userinfo":
			_ = json.NewEncoder(w).Encode(map[string]any{"sub": sub, "email": sub + "@example.com"})
		}
	})
}

// consenting sends a browser to Google to connect the node, and back with code.
func consenting(t *testing.T, h *Handler, query string) *httptest.ResponseRecorder {
	t.Helper()
	done, _ := consentingWith(t, h, query)
	return done
}

// consentingWith is consenting, with the cookies the browser came back holding.
func consentingWith(t *testing.T, h *Handler, query string) (*httptest.ResponseRecorder, []*http.Cookie) {
	t.Helper()
	start := httptest.NewRecorder()
	h.handleGoogleCloud(start, httptest.NewRequest(http.MethodGet, googleCloudPath+query, nil))
	require.Equal(t, http.StatusFound, start.Code, start.Body.String())
	sent, err := url.Parse(start.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "accounts.google.com", sent.Host)
	asked := sent.Query()
	assert.Equal(t, "client-id", asked.Get("client_id"), "the login's own client")
	assert.Contains(t, asked.Get("scope"), googleCloudScope)
	assert.Equal(t, "true", asked.Get("include_granted_scopes"))
	assert.Equal(t, callbackPath, mustPath(t, asked.Get("redirect_uri")), "the callback the login already registered")

	back := httptest.NewRequest(http.MethodGet, callbackPath+"?state="+url.QueryEscape(asked.Get("state"))+"&code=the-code", nil)
	for _, cookie := range start.Result().Cookies() {
		back.AddCookie(cookie)
	}
	done := httptest.NewRecorder()
	h.handleBindingCallback(done, back)
	return done, start.Result().Cookies()
}

// pickingHandler is a node whose ROOT's account sees projects, and keeps which
// tokens it let go.
func pickingHandler(t *testing.T, projects ...GoogleCloudProject) (*Handler, *[]connected, *[]string) {
	t.Helper()
	h, handed := cloudHandler(t)
	var letGo []string
	h.SetGoogleCloudPick(func(context.Context, string) ([]GoogleCloudProject, error) { return projects, nil },
		func(token string) { letGo = append(letGo, token) })
	return h, handed, &letGo
}

// hrefOf is the link whose text holds label on page.
func hrefOf(t *testing.T, page, label string) string {
	t.Helper()
	at := strings.Index(page, label)
	require.GreaterOrEqual(t, at, 0, page)
	start := strings.LastIndex(page[:at], `href="`)
	require.GreaterOrEqual(t, start, 0, page)
	rest := page[start+len(`href="`):]
	return html.UnescapeString(rest[:strings.Index(rest, `"`)])
}

// "i press connect, i select project, its not that hard bro"
func TestROOTPressesConnectAndSelectsTheProject(t *testing.T) {
	h, handed, _ := pickingHandler(t, GoogleCloudProject{ID: "sbvh-1", Name: "SBVH"}, GoogleCloudProject{ID: "qntx-2", Name: "QNTX"})
	googleAnswering(t, "root-sub")
	done, cookies := consentingWith(t, h, "")
	require.Equal(t, http.StatusOK, done.Code, done.Body.String())
	page := done.Body.String()
	assert.Contains(t, page, "SBVH")
	assert.Contains(t, page, "qntx-2")
	assert.Empty(t, *handed, "nothing is set up before ROOT selects")

	selected := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, hrefOf(t, page, "qntx-2"), nil)
	// As a browser sends them: only a cookie whose path holds the link. Pressed
	// for real, the ceremony's cookie stayed behind at /auth/binding.
	for _, cookie := range cookies {
		if strings.HasPrefix(req.URL.Path, cookie.Path) {
			req.AddCookie(cookie)
		}
	}
	h.handleGoogleCloud(selected, req)
	assert.Equal(t, http.StatusOK, selected.Code, selected.Body.String())
	assert.Equal(t, []connected{{"cloud-token", "qntx-2", "google:root-sub"}}, *handed)

	// What was selected is spent: the page's link does not connect twice.
	again := httptest.NewRecorder()
	h.handleGoogleCloud(again, req)
	assert.Equal(t, http.StatusForbidden, again.Code)
	assert.Len(t, *handed, 1)
}

// A browser that did not say yes selects nothing, and ROOT's selection stands.
func TestAnotherBrowserDoesNotSelectTheProject(t *testing.T) {
	h, handed, _ := pickingHandler(t, GoogleCloudProject{ID: "sbvh-1"}, GoogleCloudProject{ID: "qntx-2"})
	googleAnswering(t, "root-sub")
	done, _ := consentingWith(t, h, "")
	selected := httptest.NewRecorder()
	h.handleGoogleCloud(selected, httptest.NewRequest(http.MethodGet, hrefOf(t, done.Body.String(), "qntx-2"), nil))
	assert.Equal(t, http.StatusForbidden, selected.Code)
	assert.Empty(t, *handed)
}

// One project is no question.
func TestOneProjectIsNotAsked(t *testing.T) {
	h, handed, _ := pickingHandler(t, GoogleCloudProject{ID: "qntx-2"})
	googleAnswering(t, "root-sub")
	done := consenting(t, h, "")
	assert.Equal(t, http.StatusOK, done.Code, done.Body.String())
	assert.Equal(t, []connected{{"cloud-token", "qntx-2", "google:root-sub"}}, *handed)
}

// The list not read is said as any stop is, and ROOT's token is let go.
func TestAListThatDidNotReadLetsTheTokenGo(t *testing.T) {
	h, handed := cloudHandler(t)
	var letGo []string
	h.SetGoogleCloudPick(func(context.Context, string) ([]GoogleCloudProject, error) {
		return nil, &GoogleCloudFix{Says: "the API is off", Link: "https://console.cloud.google.com/turn-it-on", Full: "all that Google said"}
	}, func(token string) { letGo = append(letGo, token) })
	googleAnswering(t, "root-sub")
	done := consenting(t, h, "")
	assert.Equal(t, http.StatusBadGateway, done.Code)
	assert.Contains(t, done.Body.String(), "the API is off")
	assert.Empty(t, *handed)
	assert.Equal(t, []string{"cloud-token"}, letGo)
}

func mustPath(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	return parsed.Path
}

// "AND CLEARLY TELL THE USER WHATS UP", "THE BARE MINIMUM A USER NEEDS TO CONTINUE",
// "AND TRY AGAIN WHEN SHIT GOES WRONG": what is wrong in a line, where to fix it,
// Try again, and all that Google said kept under them.
func TestAConnectThatStoppedSaysWhatToDo(t *testing.T) {
	h, _ := cloudHandler(t)
	googleAnswering(t, "root-sub")
	h.SetGoogleCloud(func(context.Context, string, string, string) (string, error) {
		return "", errors.Wrap(&GoogleCloudFix{Says: "the API is off", Link: "https://console.cloud.google.com/turn-it-on", Full: "all that Google said"}, "the setup")
	})
	done := consenting(t, h, "?project=the-project")
	assert.Equal(t, http.StatusBadGateway, done.Code)
	page := done.Body.String()
	assert.Contains(t, page, "the API is off")
	// As the account that said yes, for a browser holds more than one.
	assert.Contains(t, page, `href="https://console.cloud.google.com/turn-it-on?authuser=root-sub%40example.com"`)
	assert.Contains(t, page, `href="`+googleCloudPath+`?project=the-project"`)
	assert.Contains(t, page, "Try again")
	assert.Contains(t, page, "all that Google said")
}

// Stopped on anything else, there is still Try again, and nothing to fix is offered.
func TestAConnectThatFailedOffersTryAgain(t *testing.T) {
	h, _ := cloudHandler(t)
	googleAnswering(t, "root-sub")
	h.SetGoogleCloud(func(context.Context, string, string, string) (string, error) {
		return "", errors.New("the pool was not made")
	})
	page := consenting(t, h, "").Body.String()
	assert.Contains(t, page, "the pool was not made")
	assert.Contains(t, page, `href="`+googleCloudPath+`"`)
	assert.NotContains(t, page, "Turn it on")
}

// ROOT says yes once, at Google, and the node is handed the token to set
// itself up on, in the project named.
func TestROOTsGoogleConnectsTheNode(t *testing.T) {
	h, handed := cloudHandler(t)
	googleAnswering(t, "root-sub")
	done := consenting(t, h, "?project=the-project")
	assert.Equal(t, http.StatusOK, done.Code, done.Body.String())
	assert.Equal(t, []connected{{"cloud-token", "the-project", "google:root-sub"}}, *handed)
}

// ROOT's yes for the costs goes to what wires them.
func TestROOTsYesForCostsWiresTheCosts(t *testing.T) {
	h, handed := cloudHandler(t)
	googleAnswering(t, "root-sub")
	var wired []connected
	h.SetGoogleCosts(func(_ context.Context, token, by string) (string, error) {
		wired = append(wired, connected{token, "", by})
		return "wired", nil
	})
	done := consenting(t, h, "?project=the-project&thing=costs")
	assert.Equal(t, http.StatusOK, done.Code, done.Body.String())
	assert.Contains(t, done.Body.String(), "wired")
	assert.Equal(t, []connected{{"cloud-token", "", "google:root-sub"}}, wired)
	assert.Empty(t, *handed, "the node is not connected again")
}

// 「[Opus 5.5] ROOT's yes for agy goes to what lets the node call Vertex AI.」
func TestROOTsYesForAgyLetsTheNodeRunIt(t *testing.T) {
	h, handed := cloudHandler(t)
	googleAnswering(t, "root-sub")
	var let []connected
	h.SetGoogleAgy(func(_ context.Context, token, by string) (string, error) {
		let = append(let, connected{token, "", by})
		return "let", nil
	})
	done := consenting(t, h, "?project=the-project&thing=agy")
	assert.Equal(t, http.StatusOK, done.Code, done.Body.String())
	assert.Equal(t, []connected{{"cloud-token", "", "google:root-sub"}}, let)
	assert.Empty(t, *handed, "the node is not connected again")
}

// 「[Opus 5.5] ROOT's yes for a namespace's key goes to what makes it, naming the namespace,」
// 「[Opus 5.5] and Try again asks for that namespace's key again.」
// "one project per Google spend item (each API key, the OIDC identity), with each budget halting only its own item."
func TestROOTsYesForAKeyNamesItsNamespace(t *testing.T) {
	h, handed := cloudHandler(t)
	googleAnswering(t, "root-sub")
	var made []string
	h.SetGoogleKey(func(_ context.Context, token, by, namespace string) (string, error) {
		made = append(made, token+" "+by+" "+namespace)
		return "made", nil
	})
	done := consenting(t, h, "?project=the-project&thing=key&namespace=garden")
	assert.Equal(t, http.StatusOK, done.Code, done.Body.String())
	assert.Equal(t, []string{"cloud-token google:root-sub garden"}, made)
	assert.Empty(t, *handed, "the node is not connected again")

	h.SetGoogleKey(func(context.Context, string, string, string) (string, error) {
		return "", errors.New("no project was made")
	})
	page := consenting(t, h, "?project=the-project&thing=key&namespace=garden").Body.String()
	again, err := url.Parse(hrefOf(t, page, "Try again"))
	require.NoError(t, err)
	assert.Equal(t, "key", again.Query().Get("thing"))
	assert.Equal(t, "garden", again.Query().Get("namespace"))
}

// Try again asks for the same thing: pressed for real, the thing went into the
// project's name, and the node asked Google for a project called that.
func TestTryingAgainAsksForTheSameThing(t *testing.T) {
	h, handed := cloudHandler(t)
	googleAnswering(t, "root-sub")
	h.SetGoogleCosts(func(context.Context, string, string) (string, error) {
		return "", &GoogleCloudFix{Says: "no billing", Link: "https://console.cloud.google.com/billing?project=the-project", Full: "all that Google said"}
	})
	page := consenting(t, h, "?project=the-project&thing=costs").Body.String()
	again, err := url.Parse(hrefOf(t, page, "Try again"))
	require.NoError(t, err)
	assert.Equal(t, "the-project", again.Query().Get("project"))
	assert.Equal(t, "costs", again.Query().Get("thing"))
	assert.Empty(t, *handed)
}

// "i also wish it had a button abck to qntx": back to the page it began on, on a door
// of this node, and never anywhere else.
func TestConnectingOffersTheWayBack(t *testing.T) {
	h, _ := cloudHandler(t)
	googleAnswering(t, "root-sub")
	h.SetGoogleCosts(func(context.Context, string, string) (string, error) { return "wired", nil })
	h.ownOrigins = []string{"https://web.example.org"}
	require.NoError(t, h.SetDoors(nil))

	begin := func(referer string) *httptest.ResponseRecorder {
		start := httptest.NewRecorder()
		asked := httptest.NewRequest(http.MethodGet, googleCloudPath+"?project=p&thing=costs&page=/branch/x/", nil)
		asked.Header.Set("Referer", referer)
		h.handleGoogleCloud(start, asked)
		require.Equal(t, http.StatusFound, start.Code)
		sent, err := url.Parse(start.Header().Get("Location"))
		require.NoError(t, err)
		back := httptest.NewRequest(http.MethodGet, callbackPath+"?state="+url.QueryEscape(sent.Query().Get("state"))+"&code=the-code", nil)
		for _, cookie := range start.Result().Cookies() {
			back.AddCookie(cookie)
		}
		done := httptest.NewRecorder()
		h.handleBindingCallback(done, back)
		return done
	}

	assert.Equal(t, "https://web.example.org/branch/x/", hrefOf(t, begin("https://web.example.org/").Body.String(), "Back to QNTX"))
	assert.NotContains(t, begin("https://elsewhere.example.org/").Body.String(), "Back to QNTX")
}

// Only ROOT's own Google account connects the node.
func TestAnotherGoogleAccountDoesNotConnectTheNode(t *testing.T) {
	h, handed := cloudHandler(t)
	googleAnswering(t, "someone-else")
	done := consenting(t, h, "")
	assert.Equal(t, http.StatusForbidden, done.Code)
	assert.Contains(t, done.Body.String(), "not ROOT")
	assert.Empty(t, *handed)
}

// A node without a Google login has no consent to connect it by.
func TestANodeWithoutGoogleHasNoCloudToConnect(t *testing.T) {
	h, _ := cloudHandler(t)
	h.google = nil
	recorded := httptest.NewRecorder()
	h.handleGoogleCloud(recorded, httptest.NewRequest(http.MethodGet, googleCloudPath, nil))
	assert.Equal(t, http.StatusNotFound, recorded.Code)
	assert.Contains(t, recorded.Body.String(), "auth.provider.google")
}

// The login itself stays openid email profile: the cloud is asked for only here.
func TestTheGoogleLoginAsksForNoCloud(t *testing.T) {
	login, _, err := googleProvider(OperatorClient{ID: "client-id"}).authorize(context.Background(), googleAuthHost, "https://node/auth/binding/callback")
	require.NoError(t, err)
	assert.NotContains(t, login, "cloud-platform")
}

// "A Namespace Owner logs in with their own oicd for their namespace access to agy, like how Root does for Node"

type namespaceConnected struct{ token, project, by, namespace string }

// owningHandler is a node whose garden is owned by the Google account owner-sub.
func owningHandler(t *testing.T, projects ...GoogleCloudProject) (*Handler, *[]namespaceConnected) {
	t.Helper()
	h, _, _ := pickingHandler(t, projects...)
	var handed []namespaceConnected
	h.SetGoogleNamespace(func(namespace string) (string, error) {
		if namespace != "garden" {
			return "", errors.Newf("no namespace %s", namespace)
		}
		return "google:owner-sub", nil
	}, func(_ context.Context, token, project, by, namespace string) (string, error) {
		handed = append(handed, namespaceConnected{token, project, by, namespace})
		return "garden is set up", nil
	})
	return h, &handed
}

// The namespace's owner says yes at Google, and the namespace is set up in the
// project they select.
func TestTheOwnersGoogleConnectsTheirNamespace(t *testing.T) {
	h, handed := owningHandler(t, GoogleCloudProject{ID: "gardens-project"}, GoogleCloudProject{ID: "other-project"})
	googleAnswering(t, "owner-sub")
	done, cookies := consentingWith(t, h, "?thing=agy&namespace=garden")
	require.Equal(t, http.StatusOK, done.Code, done.Body.String())
	page := done.Body.String()
	assert.Contains(t, page, "garden spend in")
	assert.Empty(t, *handed)

	selected := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, hrefOf(t, page, "gardens-project"), nil)
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	h.handleGoogleCloud(selected, req)
	assert.Equal(t, http.StatusOK, selected.Code, selected.Body.String())
	assert.Equal(t, []namespaceConnected{{"cloud-token", "gardens-project", "google:owner-sub", "garden"}}, *handed)
}

// Only the owner's own Google account connects their namespace: not ROOT's either.
func TestAnotherGoogleAccountDoesNotConnectTheNamespace(t *testing.T) {
	for _, sub := range []string{"someone-else", "root-sub"} {
		h, handed := owningHandler(t, GoogleCloudProject{ID: "gardens-project"})
		googleAnswering(t, sub)
		done := consenting(t, h, "?thing=agy&namespace=garden")
		assert.Equal(t, http.StatusForbidden, done.Code, sub)
		assert.Contains(t, done.Body.String(), "not the Google account of garden")
		assert.Empty(t, *handed, sub)
	}
}

// A namespace that stopped says Try again for that namespace.
func TestANamespaceThatStoppedOffersTryAgainForIt(t *testing.T) {
	h, _ := owningHandler(t, GoogleCloudProject{ID: "gardens-project"})
	h.SetGoogleNamespace(func(string) (string, error) { return "google:owner-sub", nil },
		func(context.Context, string, string, string, string) (string, error) {
			return "", errors.New("the pool was not made")
		})
	googleAnswering(t, "owner-sub")
	page := consenting(t, h, "?thing=agy&namespace=garden").Body.String()
	assert.Contains(t, page, "the pool was not made")
	assert.Contains(t, page, `href="`+html.EscapeString(googleCloudPath+"?namespace=garden&project=gardens-project&thing=agy")+`"`)
}

// The owner is a person: the identity their namespace names and the Google account
// that says yes are two routes of one User.
func TestTheOwnersOtherRouteConnectsTheirNamespace(t *testing.T) {
	h, handed := owningHandler(t, GoogleCloudProject{ID: "gardens-project"})
	h.users = &memUsers{held: []User{{ID: "US-GARDENER", Level: LevelUser, Accounts: []UserAccount{{CanonicalID: "https://example.org/gardener"}, {CanonicalID: "google:owner-sub"}}}}}
	h.SetGoogleNamespace(func(string) (string, error) { return "https://example.org/gardener", nil },
		func(_ context.Context, token, project, by, namespace string) (string, error) {
			*handed = append(*handed, namespaceConnected{token, project, by, namespace})
			return "garden is set up", nil
		})
	googleAnswering(t, "owner-sub")
	done := consenting(t, h, "?thing=agy&namespace=garden")
	assert.Equal(t, http.StatusOK, done.Code, done.Body.String())
	assert.Equal(t, []namespaceConnected{{"cloud-token", "gardens-project", "google:owner-sub", "garden"}}, *handed)
}
