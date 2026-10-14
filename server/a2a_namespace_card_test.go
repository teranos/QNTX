package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/a2a"
	"github.com/teranos/QNTX/server/auth"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/dynamicpb"
)

// "The Tenant and ROOT are the same"

// servingGardens is the A2A fixture holding two namespaces: garden, opted into
// an agent, and orchard, opted into none.
func servingGardens(t *testing.T) (*QNTXServer, map[auth.Level]string) {
	t.Helper()
	namedNode(t)
	srv, tokens := pluginServingServer(t, "fake")
	keyedNode(srv)
	in := gardens{store: map[string]*handed{}, owner: map[string]string{}}
	for _, name := range []string{"garden", "orchard"} {
		store, _ := createTestStore(t)
		in.store[name] = &handed{AttestationStore: store}
	}
	srv.held.SetKnown(in)
	srv.held.SetOpener(in)
	root := auth.Admitted(auth.LevelRoot)
	root.Identity = "https://example.org/root"
	require.NoError(t, srv.setAgentLine(root, "garden", agentSpec{Model: "claude-sonnet-5-5", Effort: "medium", Mode: "dontAsk", Allow: []string{}}))
	return srv, tokens
}

// "Each agent it hosts is described by an Agent Card and reached by its
// tenant" (ADR-048): GetExtendedAgentCard at garden is garden's agent, its
// interface naming garden as its tenant (§8.3.2), lacking nothing the spec
// requires, wherever the request named the tenant.
func TestANamespaceAgentIsDescribedAtItsTenant(t *testing.T) {
	srv, tokens := servingGardens(t)

	for _, path := range []string{"/a2a/garden/extendedAgentCard", "/a2a/extendedAgentCard?tenant=garden"} {
		w := askingTheExtendedCard(srv, path, tokens[auth.LevelRoot])
		require.Equal(t, http.StatusOK, w.Code, path+": "+w.Body.String())

		built, err := srv.publicCard(httptest.NewRequest(http.MethodGet, agentCardPath, nil)).Message()
		require.NoError(t, err)
		served := dynamicpb.NewMessage(built.Descriptor())
		require.NoError(t, protojson.Unmarshal(w.Body.Bytes(), served), path+": what was served is not an "+a2a.AgentCard)
		assert.Empty(t, a2a.Missing(served), path)
		require.NoError(t, a2a.Verify(served, srv.nodeDID.PublicKey, srv.nodeDID.KeyID()), path+": the card is not signed by the node")

		var card struct {
			Name       string `json:"name"`
			Interfaces []struct {
				URL    string `json:"url"`
				Tenant string `json:"tenant"`
			} `json:"supportedInterfaces"`
			Skills []struct {
				ID string `json:"id"`
			} `json:"skills"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &card))
		assert.Equal(t, "the agent of garden, claude-sonnet-5-5", card.Name, path)
		require.Len(t, card.Interfaces, 1, path)
		assert.Equal(t, "garden", card.Interfaces[0].Tenant, path+": clients MUST set the tenant the interface declares")
		assert.True(t, strings.HasSuffix(card.Interfaces[0].URL, "/a2a"), path)
		require.Len(t, card.Skills, 1, path)
		assert.Equal(t, "say", card.Skills[0].ID, path)
	}
}

// The node's own card names no tenant: it is the node, not an agent behind it.
func TestTheNodesCardNamesNoTenant(t *testing.T) {
	srv, tokens := servingGardens(t)

	w := askingTheExtendedCard(srv, "/a2a/extendedAgentCard", tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), `"tenant"`)
}

// A namespace that opted into no agent, one the node does not hold, and system
// serve no agent: each is a resource that does not exist (§3.3.2).
func TestNoAgentIsServedWhereNoneWasOptedInto(t *testing.T) {
	srv, tokens := servingGardens(t)

	for _, tenant := range []string{"orchard", "nowhere", "system"} {
		w := askingTheExtendedCard(srv, "/a2a/"+tenant+"/extendedAgentCard", tokens[auth.LevelRoot])
		assert.Equal(t, http.StatusNotFound, w.Code, tenant+": "+w.Body.String())
	}
}

// At a tenant an agent is served at, an operation the node does not do yet
// says so, as it does without one: the card declares no streaming (§3.3.4).
func TestAnAgentsTenantAnswersWhatTheNodeDoesNotDoYet(t *testing.T) {
	srv, tokens := servingGardens(t)

	w := a2aAsked(srv, http.MethodPost, "/a2a/garden/message:stream", tokens[auth.LevelRoot], sendBody("m-1", "hi", ""))
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "UNSUPPORTED_OPERATION")
}
