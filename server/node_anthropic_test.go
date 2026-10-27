package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/plugin/grpc/protocol"
	"github.com/teranos/QNTX/server/auth"
	"github.com/teranos/QNTX/server/sigil"
)

// anthropicAccepting answers as Anthropic does: an identity token verified with
// the key the node publishes, whose sub is the rule's subject, is traded for an
// access token, and the models are listed on it.
func anthropicAccepting(t *testing.T, s *QNTXServer, subject string) (base string, asserted *[]jwt.Claims) {
	t.Helper()
	keys := httptest.NewRecorder()
	s.HandleOIDCKeys(keys, httptest.NewRequest(http.MethodGet, oidcKeysPath, nil))
	var published jose.JSONWebKeySet
	require.NoError(t, json.Unmarshal(keys.Body.Bytes(), &published))
	var claimed []jwt.Claims
	served := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/oauth/token":
			body, _ := io.ReadAll(r.Body)
			var exchange struct {
				Assertion string `json:"assertion"`
			}
			require.NoError(t, json.Unmarshal(body, &exchange))
			parsed, err := jwt.ParseSigned(exchange.Assertion, []jose.SignatureAlgorithm{jose.ES256})
			require.NoError(t, err)
			var claims jwt.Claims
			require.NoError(t, parsed.Claims(published.Key(parsed.Headers[0].KeyID)[0].Key, &claims))
			claimed = append(claimed, claims)
			if claims.Subject != subject {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"Authentication failed"}}`)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"sk-ant-oat01-traded","token_type":"Bearer","expires_in":3600}`)
		case "/v1/models":
			if r.Header.Get("Authorization") != "Bearer sk-ant-oat01-traded" {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"Authentication failed"}}`)
				return
			}
			_, _ = io.WriteString(w, `{"data":[],"has_more":false}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(served.Close)
	return served.URL, &claimed
}

func rootAsksAnthropic(t *testing.T, s *QNTXServer, name string, sent sigil.Sent) *protocol.NodeAnthropic {
	t.Helper()
	admitted := auth.Admitted(auth.LevelRoot)
	admitted.Identity = "https://example.org/root"
	asked := httptest.NewRequest(http.MethodPost, "/api/anthropic", nil)
	asked = asked.WithContext(auth.WithAdmission(asked.Context(), admitted))
	answer, refused := s.anthropicSignum().Answers[name](sigil.WithCaller(asked.Context(), asked), sent)
	require.Nil(t, refused)
	holds(t, s.anthropicSignum(), name, answer)
	am, isAm := answer.(*protocol.NodeAnthropic)
	require.True(t, isAm)
	return am
}

// Each id as the Console shows it, given at the step that made it.
var (
	anOrganization = sigil.Sent{"given": "Organization ID: 377885f2-bf73-4758-9338-dcfe26b37568"}
	anIssuer       = sigil.Sent{"given": " fdis_014ooUtLKoVwFC8XnFZtMdfp "}
	aServiceAcct   = sigil.Sent{"given": "svac_node"}
	aRule          = sigil.Sent{"given": "\"fdrl_node\""}
)

// "oidc for the node"
func TestRootGivesTheNodesRuleAndAnthropicAcceptsTheNodesIdentity(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	base, asserted := anthropicAccepting(t, s, s.nodeDID.DID)
	s.anthropicBaseURL = base

	before := rootAsksAnthropic(t, s, "am", sigil.Sent{})
	assert.Equal(t, s.authHandler.PublicOrigin(), before.GetIssuer())
	assert.Equal(t, s.nodeDID.DID, before.GetSubject())
	assert.False(t, before.GetAsked(), "no ids given, nothing asked: the light is not on")

	org := rootAsksAnthropic(t, s, "set", anOrganization)
	assert.Equal(t, "377885f2-bf73-4758-9338-dcfe26b37568", org.GetOrganizationId(), "the label the Console shows it with is not the id")
	assert.False(t, org.GetAsked(), "Anthropic is not asked before the rule and the service account are known")
	issuer := rootAsksAnthropic(t, s, "set", anIssuer)
	assert.Equal(t, "fdis_014ooUtLKoVwFC8XnFZtMdfp", issuer.GetIssuerId())
	assert.Equal(t, "377885f2-bf73-4758-9338-dcfe26b37568", issuer.GetOrganizationId(), "kept: each id is given once")
	rootAsksAnthropic(t, s, "set", aServiceAcct)
	set := rootAsksAnthropic(t, s, "set", aRule)
	assert.True(t, set.GetAsked())
	assert.True(t, set.GetAccepted(), set.GetWhy())
	assert.Equal(t, "fdrl_node", set.GetFederationRuleId())
	assert.Equal(t, "https://example.org/root", set.GetSetBy())
	require.NotEmpty(t, *asserted)
	assert.Equal(t, s.nodeDID.DID, (*asserted)[0].Subject, "the node's own identity, its DID")
	assert.Equal(t, s.authHandler.PublicOrigin(), (*asserted)[0].Issuer)

	again := rootAsksAnthropic(t, s, "am", sigil.Sent{})
	assert.True(t, again.GetAccepted(), "kept: am asks again with the ids given")
}

func TestARuleThatDoesNotMatchTheNodeIsKeptWithWhyAnthropicRefused(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	base, _ := anthropicAccepting(t, s, "did:key:somebody-else")
	s.anthropicBaseURL = base

	for _, given := range []sigil.Sent{anOrganization, aServiceAcct, aRule} {
		rootAsksAnthropic(t, s, "set", given)
	}
	set := rootAsksAnthropic(t, s, "am", sigil.Sent{})
	assert.True(t, set.GetAsked())
	assert.False(t, set.GetAccepted())
	assert.Contains(t, set.GetWhy(), "fdrl_node")
	assert.Equal(t, "svac_node", set.GetServiceAccountId(), "kept all the same")
}

// "THE UI SHOULD BE OKAY WITH A SMALL MISTAKE THAT IS AUTOMATICALLY RESOLVABLE"
func TestAnIdIsPlacedByWhatItIsNotWhereItWasPasted(t *testing.T) {
	assert.Equal(t, map[string]string{"issuer_id": "fdis_014ooUtLKoVwFC8XnFZtMdfp"}, anthropicIDsIn("fdis_014ooUtLKoVwFC8XnFZtMdfp"))
	assert.Equal(t, map[string]string{"organization_id": "377885f2-bf73-4758-9338-dcfe26b37568"}, anthropicIDsIn("Organization ID: 377885f2-bf73-4758-9338-dcfe26b37568"))
	assert.Equal(t, map[string]string{"federation_rule_id": "fdrl_01DQdKZw6ZNBSii36GU5z6Gv", "service_account_id": "svac_01WZQhV4qnKBk7baz5smh5vw"},
		anthropicIDsIn("fdrl_01DQdKZw6ZNBSii36GU5z6Gv main-rule node-story svac_01WZQhV4qnKBk7baz5smh5vw"))
	// The Connect workload wizard's snippet, pasted whole from its copy button.
	assert.Equal(t, map[string]string{
		"federation_rule_id": "fdrl_01Kp4mjUuvKYj3yGjMwYfi81", "organization_id": "377885f2-bf73-4758-9338-dcfe26b37568",
		"service_account_id": "svac_01KNepFSKtHDxy3GuBdRbcEB", "workspace_id": "wrkspc_01YbvLt3KhHVJZ3hK6kA7bGA",
	}, anthropicIDsIn(`{
  "grant_type": "urn:ietf:params:oauth:grant-type:jwt-bearer",
  "assertion": "$JWT",
  "federation_rule_id": "fdrl_01Kp4mjUuvKYj3yGjMwYfi81",
  "organization_id": "377885f2-bf73-4758-9338-dcfe26b37568",
  "service_account_id": "svac_01KNepFSKtHDxy3GuBdRbcEB",
  "workspace_id": "wrkspc_01YbvLt3KhHVJZ3hK6kA7bGA"
}`))
	assert.Empty(t, anthropicIDsIn("main-rule"))
	assert.Empty(t, anthropicIDsIn("fdrl_"))
}

func TestWhatHoldsNoIdOfAnthropicsIsRefusedSayingWhatAnIdLooksLike(t *testing.T) {
	s, _, _ := runningNamespaceAgents(t)
	admitted := auth.Admitted(auth.LevelRoot)
	admitted.Identity = "https://example.org/root"
	asked := httptest.NewRequest(http.MethodPost, "/api/anthropic", nil)
	asked = asked.WithContext(auth.WithAdmission(asked.Context(), admitted))
	_, refused := s.anthropicSignum().Answers["set"](sigil.WithCaller(asked.Context(), asked), sigil.Sent{"given": "main-rule"})
	require.NotNil(t, refused)
	assert.Contains(t, refused.Says, "fdrl_")
}
