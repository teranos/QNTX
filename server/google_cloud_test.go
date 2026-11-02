package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// "Given that I am ROOT and my google identity is the ROOT identity"

// standInGoogle is Google Cloud as the node meets it: one account's projects,
// operations Google finishes on its next ask, a pool, a provider, a service
// account and its policy, the token exchange, and acting as the account.
type standInGoogle struct {
	t        *testing.T
	mu       sync.Mutex
	projects []googleProject
	enabled  []string
	pool     bool
	provider map[string]any
	account  bool
	policy   map[string]any
	revoked  []string
	jwks     jose.JSONWebKeySet
}

func googleStandingIn(t *testing.T, s *QNTXServer, projects ...googleProject) *standInGoogle {
	t.Helper()
	keys := httptest.NewRecorder()
	s.HandleOIDCKeys(keys, httptest.NewRequest(http.MethodGet, oidcKeysPath, nil))
	g := &standInGoogle{t: t, projects: projects, policy: map[string]any{"etag": "e1"}}
	require.NoError(t, json.Unmarshal(keys.Body.Bytes(), &g.jwks))
	served := httptest.NewServer(http.HandlerFunc(g.answer))
	t.Cleanup(served.Close)
	asksAgain := googleAsksAgain
	googleAsksAgain = time.Millisecond
	t.Cleanup(func() { googleAsksAgain = asksAgain })
	was := []string{googleResourceManager, googleServiceUsage, googleIAM, googleSTS, googleCredentials, googleRevoke}
	googleResourceManager, googleServiceUsage, googleIAM, googleSTS, googleCredentials, googleRevoke = served.URL, served.URL, served.URL, served.URL, served.URL, served.URL+"/revoke"
	t.Cleanup(func() {
		googleResourceManager, googleServiceUsage, googleIAM, googleSTS, googleCredentials, googleRevoke = was[0], was[1], was[2], was[3], was[4], was[5]
	})
	return g
}

const thePool = "projects/123/locations/global/workloadIdentityPools/qntx"

func (g *standInGoogle) answer(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	conflict := func() {
		w.WriteHeader(http.StatusConflict)
		write(map[string]any{"error": map[string]any{"status": "ALREADY_EXISTS"}})
	}
	path := r.URL.Path
	rootOnly := func() bool {
		if r.Header.Get("Authorization") != "Bearer root-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	switch {
	case path == "/v1/projects" && r.Method == http.MethodGet:
		if rootOnly() {
			write(map[string]any{"projects": g.projects})
		}
	case strings.HasPrefix(path, "/v1/projects/123/services/") && strings.HasSuffix(path, ":enable"):
		if rootOnly() {
			service := strings.TrimSuffix(strings.TrimPrefix(path, "/v1/projects/123/services/"), ":enable")
			g.enabled = append(g.enabled, service)
			write(googleOperation{Name: "operations/enable-" + service})
		}
	case strings.HasPrefix(path, "/v1/operations/") || strings.Contains(path, "/operations/"):
		write(googleOperation{Name: strings.TrimPrefix(path, "/v1/"), Done: true})
	case path == "/v1/projects/123/locations/global/workloadIdentityPools" && r.URL.Query().Get("workloadIdentityPoolId") == "qntx":
		if !rootOnly() {
			return
		}
		if g.pool {
			conflict()
			return
		}
		g.pool = true
		write(googleOperation{Name: thePool + "/operations/made"})
	case path == "/v1/"+thePool+"/providers" && r.URL.Query().Get("workloadIdentityPoolProviderId") == "node":
		if !rootOnly() {
			return
		}
		if g.provider != nil {
			conflict()
			return
		}
		require.NoError(g.t, json.NewDecoder(r.Body).Decode(&g.provider))
		write(googleOperation{Name: thePool + "/providers/node/operations/made"})
	case path == "/v1/"+thePool+"/providers/node" && r.Method == http.MethodGet:
		write(g.provider)
	case path == "/v1/projects/the-project/serviceAccounts":
		if !rootOnly() {
			return
		}
		if g.account {
			conflict()
			return
		}
		g.account = true
		write(map[string]any{"email": "qntx-node@the-project.iam.gserviceaccount.com"})
	case path == "/v1/projects/the-project/serviceAccounts/qntx-node@the-project.iam.gserviceaccount.com:getIamPolicy":
		if rootOnly() {
			write(g.policy)
		}
	case path == "/v1/projects/the-project/serviceAccounts/qntx-node@the-project.iam.gserviceaccount.com:setIamPolicy":
		if !rootOnly() {
			return
		}
		var set struct {
			Policy map[string]any `json:"policy"`
		}
		require.NoError(g.t, json.NewDecoder(r.Body).Decode(&set))
		g.policy = set.Policy
		write(g.policy)
	case path == "/revoke":
		g.revoked = append(g.revoked, r.URL.Query().Get("token"))
	case path == "/v1/token":
		g.exchange(w, r)
	case path == "/v1/projects/-/serviceAccounts/qntx-node@the-project.iam.gserviceaccount.com:generateAccessToken":
		subject := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer sts-")
		if !g.bound(subject) {
			w.WriteHeader(http.StatusForbidden)
			write(map[string]any{"error": map[string]any{"message": "Permission 'iam.serviceAccounts.getAccessToken' denied"}})
			return
		}
		write(map[string]any{"accessToken": "acting-as-the-node"})
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// exchange is Google's token exchange: an identity token the node's published
// key verifies, for the provider's audience, traded for an access token.
func (g *standInGoogle) exchange(w http.ResponseWriter, r *http.Request) {
	require.NoError(g.t, r.ParseForm())
	assert.Equal(g.t, "urn:ietf:params:oauth:grant-type:token-exchange", r.PostForm.Get("grant_type"))
	assert.Equal(g.t, "urn:ietf:params:oauth:token-type:jwt", r.PostForm.Get("subject_token_type"))
	parsed, err := jwt.ParseSigned(r.PostForm.Get("subject_token"), []jose.SignatureAlgorithm{jose.ES256})
	require.NoError(g.t, err)
	var claims jwt.Claims
	require.NoError(g.t, parsed.Claims(g.jwks.Key(parsed.Headers[0].KeyID)[0].Key, &claims))
	audience := "//iam.googleapis.com/" + thePool + "/providers/node"
	if g.provider == nil || r.PostForm.Get("audience") != audience || !slices.Contains([]string(claims.Audience), audience) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"The audience in ID Token does not match the expected audience."}`)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "sts-" + claims.Subject, "token_type": "Bearer"})
}

// bound is whether subject may act as the node's service account.
func (g *standInGoogle) bound(subject string) bool {
	bindings, _ := g.policy["bindings"].([]any)
	for _, b := range bindings {
		binding, _ := b.(map[string]any)
		members, _ := binding["members"].([]any)
		if binding["role"] == "roles/iam.workloadIdentityUser" && slices.Contains(members, any("principal://iam.googleapis.com/"+thePool+"/subject/"+subject)) {
			return true
		}
	}
	return false
}

var theProject = googleProject{ProjectID: "the-project", ProjectNumber: "123"}

func rootAsksGoogle(t *testing.T, s *QNTXServer) *protocol.NodeGoogle {
	t.Helper()
	admitted := auth.Admitted(auth.LevelRoot)
	admitted.Identity = "google:root-sub"
	asked := httptest.NewRequest(http.MethodGet, "/api/google", nil)
	asked = asked.WithContext(auth.WithAdmission(asked.Context(), admitted))
	answer, refused := s.googleSignum().Answers["am"](sigil.WithCaller(asked.Context(), asked), sigil.Sent{})
	require.Nil(t, refused)
	holds(t, s.googleSignum(), "am", answer)
	return answer.(*protocol.NodeGoogle)
}

// ROOT says yes once, and the node sets itself up: the APIs, a pool whose
// provider trusts the node's issuer, a service account its DID acts as. Google
// then trades the node's own identity token, and ROOT's token is let go.
func TestROOTsYesSetsTheNodeUpAtGoogleCloud(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	g := googleStandingIn(t, s, theProject)

	before := rootAsksGoogle(t, s)
	assert.Equal(t, s.authHandler.PublicOrigin(), before.GetIssuer())
	assert.Equal(t, s.nodeDID.DID, before.GetSubject())
	assert.Equal(t, s.authHandler.PublicOrigin()+auth.GoogleCloudPath(), before.GetConnect())
	assert.False(t, before.GetAsked(), "not connected, nothing asked")

	said, err := s.connectGoogleCloud(context.Background(), "root-token", "", "google:root-sub")
	require.NoError(t, err)
	assert.Contains(t, said, "the-project")
	assert.ElementsMatch(t, googleServices, g.enabled)
	assert.Equal(t, map[string]any{"issuerUri": s.authHandler.PublicOrigin()}, g.provider["oidc"])
	assert.Equal(t, map[string]any{"google.subject": "assertion.sub"}, g.provider["attributeMapping"])
	assert.True(t, g.bound(s.nodeDID.DID), "the node's DID acts as its service account")
	assert.Equal(t, []string{"root-token"}, g.revoked, "the node keeps no credential of ROOT's")

	am := rootAsksGoogle(t, s)
	assert.True(t, am.GetAsked())
	assert.True(t, am.GetAccepted(), am.GetWhy())
	assert.Equal(t, "the-project", am.GetProjectId())
	assert.Equal(t, "123", am.GetProjectNumber())
	assert.Equal(t, thePool, am.GetPool())
	assert.Equal(t, thePool+"/providers/node", am.GetProvider())
	assert.Equal(t, "qntx-node@the-project.iam.gserviceaccount.com", am.GetServiceAccount())
	assert.Equal(t, "google:root-sub", am.GetSetBy())
}

// Connecting again keeps what was made, and binds the node once.
func TestConnectingAgainKeepsWhatWasMade(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	g := googleStandingIn(t, s, theProject)
	_, err := s.connectGoogleCloud(context.Background(), "root-token", "", "google:root-sub")
	require.NoError(t, err)
	_, err = s.connectGoogleCloud(context.Background(), "root-token", "", "google:root-sub")
	require.NoError(t, err)
	bindings := g.policy["bindings"].([]any)
	require.Len(t, bindings, 1)
	assert.Len(t, bindings[0].(map[string]any)["members"], 1)
	assert.True(t, rootAsksGoogle(t, s).GetAccepted())
}

// A provider made before for another issuer is not taken for the node's.
func TestAProviderTrustingAnotherIssuerIsNotTheNodes(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	g := googleStandingIn(t, s, theProject)
	g.pool, g.provider = true, map[string]any{"oidc": map[string]any{"issuerUri": "https://another.example"}}
	_, err := s.connectGoogleCloud(context.Background(), "root-token", "", "google:root-sub")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "https://another.example")
	assert.Equal(t, []string{"root-token"}, g.revoked)
}

// ROOT's account seeing several projects names one to connect in.
func TestSeveralProjectsAreNamedNotGuessed(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	googleStandingIn(t, s, googleProject{ProjectID: "another", ProjectNumber: "9"}, theProject)
	_, err := s.connectGoogleCloud(context.Background(), "root-token", "", "google:root-sub")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "?project=")
	assert.Contains(t, err.Error(), "the-project")

	_, err = s.connectGoogleCloud(context.Background(), "root-token", "the-project", "google:root-sub")
	require.NoError(t, err)
	assert.Equal(t, "the-project", rootAsksGoogle(t, s).GetProjectId())
}

// The identity Google trades is for Google's audience, not Anthropic's.
func TestTheNodesIdentityTokenIsForWhomItIsShown(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	i, err := s.theIssuer()
	require.NoError(t, err)
	signed, err := i.identityTokenFor(s.nodeDID.DID, auth.NamespaceSystem, googleAudience("123"))
	require.NoError(t, err)
	parsed, err := jwt.ParseSigned(signed, []jose.SignatureAlgorithm{jose.ES256})
	require.NoError(t, err)
	var claims jwt.Claims
	require.NoError(t, parsed.UnsafeClaimsWithoutVerification(&claims))
	assert.Equal(t, jwt.Audience{"//iam.googleapis.com/" + thePool + "/providers/node"}, claims.Audience)
	anthropic, err := i.identityToken(s.nodeDID.DID, auth.NamespaceSystem)
	require.NoError(t, err)
	parsed, err = jwt.ParseSigned(anthropic, []jose.SignatureAlgorithm{jose.ES256})
	require.NoError(t, err)
	require.NoError(t, parsed.UnsafeClaimsWithoutVerification(&claims))
	assert.Equal(t, jwt.Audience{identityTokenAudience}, claims.Audience)
}
