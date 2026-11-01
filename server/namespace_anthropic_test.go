package server

import (
	"context"
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/access"
	"github.com/teranos/QNTX/internal/slug"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

func askNamespaceAnthropic(t *testing.T, s *QNTXServer, who string, level auth.Level, acting []string, name, namespace string, sent sigil.Sent) any {
	t.Helper()
	admitted := auth.Admitted(level, acting...)
	admitted.Identity = who
	sent["namespace"] = namespace
	request := httptest.NewRequest(http.MethodPost, "/api/agents/"+namespace+"/anthropic", nil)
	request = request.WithContext(auth.WithAdmission(request.Context(), admitted))
	answer, refused := s.namespaceAnthropicSignum().Answers[name](sigil.WithCaller(request.Context(), request), sent)
	require.Nil(t, refused)
	return answer
}

func TestNamespaceAnthropicFederationIsKeptOnlyInItsNamespace(t *testing.T) {
	s, _, stores := runningNamespaceAgents(t)
	console, base := standInConsole(t)
	s.anthropicBaseURL = base

	issuerInfo, ok := askNamespaceAnthropic(t, s, "https://example.org/root", auth.LevelRoot, nil, "issuer", "garden", sigil.Sent{}).(*protocol.NodeAnthropic)
	require.True(t, ok)
	assert.Equal(t, s.authHandler.PublicOrigin()+"/oidc/garden", issuerInfo.GetIssuer())
	assert.NotEmpty(t, issuerInfo.GetSubject())

	given := sigil.Sent{"given": "377885f2-bf73-4758-9338-dcfe26b37568 fdis_garden svac_garden fdrl_garden"}
	registered, ok := askNamespaceAnthropic(t, s, "https://example.org/root", auth.LevelRoot, nil, "set", "garden", given).(*protocol.NodeAnthropic)
	require.True(t, ok)
	assert.True(t, registered.GetAccepted(), registered.GetWhy())
	assert.Equal(t, issuerInfo.GetIssuer(), registered.GetIssuer())
	assert.Equal(t, issuerInfo.GetSubject(), registered.GetSubject())
	assert.Equal(t, "fdrl_garden", registered.GetFederationRuleId())

	other, ok := askNamespaceAnthropic(t, s, "https://example.org/root", auth.LevelRoot, nil, "am", "orchard", sigil.Sent{}).(*protocol.NodeAnthropic)
	require.True(t, ok)
	assert.NotEqual(t, issuerInfo.GetIssuer(), other.GetIssuer())
	assert.Empty(t, other.GetFederationRuleId(), "federation configuration does not cross namespaces")
	require.Len(t, stores.store["garden"].rows, 1)
	assert.Equal(t, []string{"garden"}, stores.store["garden"].rows[0].Contexts)
	assert.Empty(t, stores.store["orchard"].rows)
	system, _ := s.held.WriteWhatTheNodeKnowsOfItself()
	assert.Empty(t, system.(*handed).rows)
	assert.False(t, console.sawAPIKey)
}

func TestNamespaceAnthropicFederationConfigurationRequiresAPerson(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	for _, admitted := range []auth.Admission{
		func() auth.Admission {
			token := auth.Admitted(auth.LevelToken, "garden")
			token.Identity = "did:key:token"
			token.Grant = &access.Grant{}
			return token
		}(),
		func() auth.Admission {
			connector := auth.Admitted(auth.LevelUser, "garden")
			connector.Identity = "https://example.org/user"
			connector.ClientDID = "did:key:zconnector"
			return connector
		}(),
	} {
		ctx := auth.WithAdmission(context.Background(), admitted)
		_, refused := namespaceAnthropicAsking(ctx, sigil.Sent{"namespace": "garden"}, s)
		require.NotNil(t, refused)
		assert.Equal(t, sigil.NotAllowed, refused.GetWhy())
	}
}

func TestNamespaceAnthropicAgentRunsRemotelyUsingFederationWithoutAnAPIKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "not-a-real-api-key")
	s, _, stores := runningNamespaceAgents(t)
	console, base := standInConsole(t)
	s.anthropicBaseURL = base
	subject, err := s.namespaceAnthropicSubject("garden")
	require.NoError(t, err)
	issuer, err := s.namespaceIssuer("garden")
	require.NoError(t, err)

	configured, ok := askNamespaceAnthropic(t, s, "https://example.org/root", auth.LevelRoot, nil, "set", "garden",
		sigil.Sent{"given": "377885f2-bf73-4758-9338-dcfe26b37568 fdis_garden svac_garden fdrl_garden"}).(*protocol.NodeAnthropic)
	require.True(t, ok)
	require.True(t, configured.GetAccepted(), configured.GetWhy())

	am, refused := rootAsksAm(s, "set", sigil.Sent{"namespace": "garden", "harness": agentLineAnthropic, "model": "claude-sonnet-5-5"})
	require.Nil(t, refused)
	assert.Equal(t, "Anthropic workload identity federation", am.GetAgent().GetAuthMethod())
	assert.True(t, am.GetAgent().GetSignedIn(), am.GetAgent().GetNotReady())

	answer, refused := answeredAs(s, visitor, auth.LevelUser, []string{"garden"}, "say",
		sigil.Sent{"namespace": "garden", "harness": agentLineAnthropic, "says": "How long has the box been up?"})
	require.Nil(t, refused)
	said, ok := answer.(*protocol.NamespaceAgentSaid)
	require.True(t, ok)
	assert.Equal(t, "the element answered at Anthropic", said.GetClaude().GetAnswer())
	assert.Equal(t, "claude-sonnet-5-5", said.GetClaude().GetModel())

	console.mu.Lock()
	defer console.mu.Unlock()
	require.NotEmpty(t, console.assertions)
	parsed, err := jwt.ParseSigned(console.assertions[0], []jose.SignatureAlgorithm{jose.ES256})
	require.NoError(t, err)
	var claims jwt.Claims
	var custom struct {
		Namespace string `json:"namespace"`
	}
	require.NoError(t, parsed.Claims(issuer.jwk.Key, &claims, &custom))
	assert.Equal(t, issuer.url, claims.Issuer)
	assert.Equal(t, subject, claims.Subject)
	assert.Equal(t, "garden", custom.Namespace)
	require.NotEmpty(t, console.bodies["oauth"])
	oauth := console.bodies["oauth"][0]
	assert.Equal(t, "fdrl_garden", oauth["federation_rule_id"])
	assert.Equal(t, "svac_garden", oauth["service_account_id"])
	assert.Equal(t, "377885f2-bf73-4758-9338-dcfe26b37568", oauth["organization_id"])
	assert.NotEmpty(t, console.bodies["session"], "the remote Anthropic session was created")
	assert.Equal(t, "qntx-namespace-garden", console.bodies["agent"][0]["name"])
	assert.False(t, console.sawAPIKey)
	assert.NotEmpty(t, stores.store["garden"].rows, "the namespace owns its agent and transcript attestations")
	system, _ := s.held.WriteWhatTheNodeKnowsOfItself()
	assert.Empty(t, system.(*handed).rows)
}

func TestNamespaceManagedEnvironmentsAreScopedByAnthropicOrganization(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	console, base := standInConsole(t)
	s.anthropicBaseURL = base

	for _, item := range []struct {
		namespace, organization string
	}{
		{namespace: "garden", organization: "377885f2-bf73-4758-9338-dcfe26b37568"},
		{namespace: "orchard", organization: "84f05368-5ce6-4d93-bec6-1a17f0c37767"},
		{namespace: "meadow", organization: "377885f2-bf73-4758-9338-dcfe26b37568"},
	} {
		kept := anthropicKept{ids: map[string]string{
			"organization_id": item.organization, "service_account_id": "svac_" + item.namespace,
			"federation_rule_id": "fdrl_" + item.namespace,
		}}
		client, err := s.namespaceAnthropicClient(item.namespace, kept)
		require.NoError(t, err)
		_, err = s.managedEnvironment(context.Background(), client, &rootAgent{namespace: item.namespace}, kept)
		require.NoError(t, err)
	}

	console.mu.Lock()
	assert.Equal(t, 2, console.made["environment"], "different organizations get different environments, while namespaces in one organization share theirs")
	console.mu.Unlock()
}

func TestNamespaceIssuerKeysAreDerivedFromDistinctNamespaceInputs(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 9
	node := ed25519.NewKeyFromSeed(seed)
	first, err := newIssuer("https://q.example/oidc/garden", mustDerivedIssuerKey(t, node, "garden"))
	require.NoError(t, err)
	second, err := newIssuer("https://q.example/oidc/orchard", mustDerivedIssuerKey(t, node, "orchard"))
	require.NoError(t, err)
	assert.NotEqual(t, first.jwk.KeyID, second.jwk.KeyID)
}

func mustDerivedIssuerKey(t *testing.T, node ed25519.PrivateKey, namespace string) ed25519.PrivateKey {
	t.Helper()
	key, err := access.DeriveKey(node, "oidc:namespace:"+slug.Of(namespace))
	require.NoError(t, err)
	return key
}
