package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// "A Namespace Owner logs in with their own oicd for their namespace access to agy, like how Root does for Node"

// ownersGoogle is Google Cloud as a namespace's owner meets it: one project of
// theirs, and what the node makes in it.
type ownersGoogle struct {
	t        *testing.T
	mu       sync.Mutex
	enabled  []string
	pool     bool
	provider map[string]any
	account  string
	policy   map[string]any
	project  []any
	revoked  []string
	jwks     jose.JSONWebKeySet
}

var gardensProject = googleProject{ProjectID: "gardens-project", ProjectNumber: "456"}

const gardensPool = "projects/456/locations/global/workloadIdentityPools/qntx"

func ownersGoogleStandingIn(t *testing.T, s *QNTXServer) *ownersGoogle {
	t.Helper()
	keys := httptest.NewRecorder()
	s.HandleOIDCKeys(keys, httptest.NewRequest(http.MethodGet, oidcKeysPath, nil))
	g := &ownersGoogle{t: t, policy: map[string]any{}}
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

func (g *ownersGoogle) answer(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	defer g.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	write := func(v any) { _ = json.NewEncoder(w).Encode(v) }
	path := r.URL.Path
	owners := func() bool {
		if r.Header.Get("Authorization") != "Bearer owner-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return false
		}
		return true
	}
	account := namespaceGoogleAccount("garden") + "@gardens-project.iam.gserviceaccount.com"
	switch {
	case path == "/v1/projects/gardens-project" && r.Method == http.MethodGet:
		if owners() {
			write(gardensProject)
		}
	case strings.Contains(path, "/services/") && strings.HasSuffix(path, ":enable"):
		if owners() {
			service := strings.TrimSuffix(path[strings.Index(path, "/services/")+len("/services/"):], ":enable")
			g.enabled = append(g.enabled, service)
			write(googleOperation{Name: "operations/enable-" + service})
		}
	case strings.Contains(path, "/operations/"):
		write(googleOperation{Name: strings.TrimPrefix(path, "/v1/"), Done: true})
	case path == "/v1/projects/456/locations/global/workloadIdentityPools":
		if owners() {
			g.pool = true
			write(googleOperation{Name: gardensPool + "/operations/made"})
		}
	case path == "/v1/"+gardensPool+"/providers/"+googleNamespaceProvider && r.Method == http.MethodGet:
		write(g.provider)
	case path == "/v1/"+gardensPool+"/providers" && r.URL.Query().Get("workloadIdentityPoolProviderId") == googleNamespaceProvider:
		if g.provider != nil {
			w.WriteHeader(http.StatusConflict)
			write(map[string]any{"error": map[string]any{"status": "ALREADY_EXISTS"}})
			return
		}
		if owners() {
			require.NoError(g.t, json.NewDecoder(r.Body).Decode(&g.provider))
			write(googleOperation{Name: gardensPool + "/providers/namespaces/operations/made"})
		}
	case path == "/v1/projects/gardens-project/serviceAccounts":
		if owners() {
			var made struct {
				AccountID string `json:"accountId"`
			}
			require.NoError(g.t, json.NewDecoder(r.Body).Decode(&made))
			g.account = made.AccountID
			write(map[string]any{"email": account})
		}
	case path == "/v1/projects/gardens-project/serviceAccounts/"+account+":getIamPolicy":
		if owners() {
			write(g.policy)
		}
	case path == "/v1/projects/gardens-project/serviceAccounts/"+account+":setIamPolicy":
		if owners() {
			var set struct {
				Policy map[string]any `json:"policy"`
			}
			require.NoError(g.t, json.NewDecoder(r.Body).Decode(&set))
			g.policy = set.Policy
			write(g.policy)
		}
	case path == "/v1/projects/gardens-project:getIamPolicy":
		if owners() {
			write(map[string]any{"bindings": g.project})
		}
	case path == "/v1/projects/gardens-project:setIamPolicy":
		if owners() {
			var set struct {
				Policy struct {
					Bindings []any `json:"bindings"`
				} `json:"policy"`
			}
			require.NoError(g.t, json.NewDecoder(r.Body).Decode(&set))
			g.project = set.Policy.Bindings
			write(map[string]any{"bindings": g.project})
		}
	case path == "/revoke":
		g.revoked = append(g.revoked, r.URL.Query().Get("token"))
	case path == "/v1/token":
		g.exchange(w, r)
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// exchange trades an identity token for the namespaces' provider, and hands back
// the namespace it was for.
func (g *ownersGoogle) exchange(w http.ResponseWriter, r *http.Request) {
	require.NoError(g.t, r.ParseForm())
	parsed, err := jwt.ParseSigned(r.PostForm.Get("subject_token"), []jose.SignatureAlgorithm{jose.ES256})
	require.NoError(g.t, err)
	var claims jwt.Claims
	var namespace struct {
		Namespace string `json:"namespace"`
	}
	require.NoError(g.t, parsed.Claims(g.jwks.Key(parsed.Headers[0].KeyID)[0].Key, &claims, &namespace))
	audience := "//iam.googleapis.com/" + gardensPool + "/providers/" + googleNamespaceProvider
	if r.PostForm.Get("audience") != audience || !slices.Contains([]string(claims.Audience), audience) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "sts-" + namespace.Namespace, "token_type": "Bearer"})
}

// mayActAs is whether namespace may act as the namespace's service account.
func (g *ownersGoogle) mayActAs(namespace string) bool {
	bindings, _ := g.policy["bindings"].([]any)
	for _, b := range bindings {
		binding, _ := b.(map[string]any)
		members, _ := binding["members"].([]any)
		if binding["role"] == "roles/iam.workloadIdentityUser" && slices.Contains(members, any("principalSet://iam.googleapis.com/"+gardensPool+"/attribute.namespace/"+namespace)) {
			return true
		}
	}
	return false
}

// gardenConnected is garden connected to its owner's Google Cloud.
func gardenConnected(t *testing.T, s *QNTXServer) *ownersGoogle {
	t.Helper()
	g := ownersGoogleStandingIn(t, s)
	_, err := s.connectGoogleNamespace(context.Background(), "owner-token", "gardens-project", gardener, "garden")
	require.NoError(t, err)
	return g
}

// The owner's yes sets the namespace up in the owner's project: the namespace alone
// acts as its service account, which may call Vertex AI, and the owner's token is
// let go.
func TestTheOwnersYesSetsTheNamespaceUpInTheirProject(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	g := gardenConnected(t, s)

	assert.Contains(t, g.enabled, googleModelService)
	assert.True(t, g.pool)
	issuer, err := s.theIssuer()
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"issuerUri": issuer.url}, g.provider["oidc"])
	assert.Equal(t, "assertion.namespace", g.provider["attributeMapping"].(map[string]any)["attribute.namespace"])
	assert.Equal(t, namespaceGoogleAccount("garden"), g.account)
	assert.LessOrEqual(t, len(g.account), 30, "Google names a service account in thirty characters")
	assert.True(t, g.mayActAs("garden"))
	assert.False(t, g.mayActAs("orchard"))
	assert.Equal(t, []any{map[string]any{"role": googleRunsModels, "members": []any{"serviceAccount:" + namespaceGoogleAccount("garden") + "@gardens-project.iam.gserviceaccount.com"}}}, g.project)
	assert.Equal(t, []string{"owner-token"}, g.revoked)

	kept, held, err := s.namespaceGoogleLine("garden")
	require.NoError(t, err)
	require.True(t, held)
	assert.Equal(t, gardensProject, kept.project)
	assert.Equal(t, gardener, kept.setBy)
	_, held, err = s.googleLine()
	require.NoError(t, err)
	assert.False(t, held, "the node is not connected by a namespace's owner")
}

// What the namespace's agent signs in on at Google is a token the node issued for
// that namespace, which the owner's provider trades.
func TestTheNamespacesAgentIsTheNamespaceAtGoogle(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	gardenConnected(t, s)
	agent, refused := s.holdNamespaceAgent("garden", "gemini-3.8-flash", gardener)
	require.Nil(t, refused)

	google, refused := s.agyAsTheNamespace(agent)
	require.Nil(t, refused)
	assert.Equal(t, "gardens-project", google.Project)
	var credentials map[string]any
	file, err := os.ReadFile(google.Credentials)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(file, &credentials))
	assert.Equal(t, namespaceGoogleAudience("456"), credentials["audience"])
	assert.Contains(t, credentials["service_account_impersonation_url"], namespaceGoogleAccount("garden"))

	token, err := os.ReadFile(agyTokenFile(agent))
	require.NoError(t, err)
	exchanged, err := http.PostForm(googleSTS+"/v1/token", map[string][]string{"audience": {namespaceGoogleAudience("456")}, "subject_token": {string(token)}})
	require.NoError(t, err)
	defer exchanged.Body.Close()
	var answered struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.NewDecoder(exchanged.Body).Decode(&answered))
	assert.Equal(t, "sts-garden", answered.AccessToken)
}

// A namespace its owner has not connected says where its owner connects it.
func TestANamespaceNotConnectedSaysWhereItsOwnerConnectsIt(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	agent, refused := s.holdNamespaceAgent("garden", "gemini-3.8-flash", gardener)
	require.Nil(t, refused)
	_, refused = s.agyAsTheNamespace(agent)
	require.NotNil(t, refused)
	assert.Contains(t, refused.GetSays(), "/auth/google/cloud?namespace=garden&thing=agy")
}

// The owner is who the namespace's definition names.
func TestTheNamespacesOwnerIsWhoItsDefinitionNames(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	owner, err := s.namespaceGoogleOwner("garden")
	require.NoError(t, err)
	assert.Equal(t, gardener, owner)
}

// A namespace whose owner changed spends nothing of the owner who connected it: its
// new owner connects it.
func TestANewOwnerConnectsTheNamespaceAgain(t *testing.T) {
	s, _, in := runningNamespaceAgents(t)
	gardenConnected(t, s)
	in.owner["garden"] = visitor
	_, held, err := s.namespaceGoogleLine("garden")
	require.NoError(t, err)
	assert.False(t, held)
	agent, refused := s.holdNamespaceAgent("garden", "gemini-3.8-flash", gardener)
	require.Nil(t, refused)
	_, refused = s.agyAsTheNamespace(agent)
	require.NotNil(t, refused)
	assert.Contains(t, refused.GetSays(), "/auth/google/cloud?namespace=garden&thing=agy")
}

// A namespaces' provider made before is kept only when it maps the namespace from
// the node's claim, which the binding is to.
func TestAProviderNotMappingTheNamespaceIsNotKept(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	g := ownersGoogleStandingIn(t, s)
	issuer, err := s.theIssuer()
	require.NoError(t, err)
	g.provider = map[string]any{"oidc": map[string]any{"issuerUri": issuer.url}, "attributeMapping": map[string]any{"google.subject": "assertion.sub"}}
	_, err = s.connectGoogleNamespace(context.Background(), "owner-token", "gardens-project", gardener, "garden")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "attribute.namespace")
	_, held, err := s.namespaceGoogleLine("garden")
	require.NoError(t, err)
	assert.False(t, held)

	g.provider["attributeMapping"] = map[string]any{"google.subject": "assertion.sub", "attribute.namespace": "assertion.namespace"}
	_, err = s.connectGoogleNamespace(context.Background(), "owner-token", "gardens-project", gardener, "garden")
	require.NoError(t, err, "one that maps it is kept")
}
