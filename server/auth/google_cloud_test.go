package auth

import (
	"context"
	"encoding/json"
	errors "github.com/teranos/sacred-error"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	return done
}

func mustPath(t *testing.T, raw string) string {
	t.Helper()
	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	return parsed.Path
}

// An OAuth client ID opens with the number of the project it was made in: the
// project ROOT "already set up the Google login in".
func TestTheLoginsClientNamesItsProject(t *testing.T) {
	assert.Equal(t, "606051110730", loginProject("606051110730-abc.apps.googleusercontent.com"))
	assert.Equal(t, "", loginProject("client-id"))
	assert.Equal(t, "", loginProject(""))
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
	assert.Contains(t, page, `href="https://console.cloud.google.com/turn-it-on"`)
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
