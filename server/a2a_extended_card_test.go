package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/a2a"
	"github.com/teranos/QNTX/server/auth"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/dynamicpb"
)

// "Think in the context of A2A and multinode QNTX, In terms of node specific
// Agent cards, and discoverability of namespace agents"

// askingTheExtendedCard is GET /a2a/extendedAgentCard as the bearer of token.
func askingTheExtendedCard(srv *QNTXServer, path, token string) *httptest.ResponseRecorder {
	r := asBearer(http.MethodGet, path, token)
	r.Header.Set("A2A-Version", "1.0")
	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, r)
	return w
}

// The public card says the extended card is there to ask (§3.3.4), so a
// client knows to ask for it once it holds a credential (§3.1.11).
func TestThePublicCardDeclaresTheExtendedCard(t *testing.T) {
	namedNode(t)
	srv, _ := pluginServingServer(t, "fake")
	keyedNode(srv)

	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, httptest.NewRequest(http.MethodGet, agentCardPath, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var card struct {
		Capabilities struct {
			ExtendedAgentCard bool `json:"extendedAgentCard"`
		} `json:"capabilities"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &card))
	assert.True(t, card.Capabilities.ExtendedAgentCard)
}

// GetExtendedAgentCard is the card its caller reaches: an AgentCard lacking
// nothing the spec requires, a skill per signum ROOT's lines reach over A2A,
// at the host the caller reached. It is the card am node shows.
func TestTheExtendedCardIsWhatTheCallerReaches(t *testing.T) {
	namedNode(t)
	srv, tokens := pluginServingServer(t, "fake")

	w := askingTheExtendedCard(srv, "/a2a/extendedAgentCard", tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "application/a2a+json", w.Header().Get("Content-Type"))

	built, err := srv.publicCard(httptest.NewRequest(http.MethodGet, agentCardPath, nil)).Message()
	require.NoError(t, err)
	served := dynamicpb.NewMessage(built.Descriptor())
	require.NoError(t, protojson.Unmarshal(w.Body.Bytes(), served), "what was served is not an "+a2a.AgentCard)
	assert.Empty(t, a2a.Missing(served))

	var card struct {
		Capabilities struct {
			ExtendedAgentCard bool `json:"extendedAgentCard"`
		} `json:"capabilities"`
		Skills []struct {
			ID string `json:"id"`
		} `json:"skills"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &card))
	// "Clients retrieving this extended card SHOULD replace their cached
	// public Agent Card", so it keeps saying it is there.
	assert.True(t, card.Capabilities.ExtendedAgentCard)
	var ids []string
	for _, skill := range card.Skills {
		ids = append(ids, skill.ID)
	}
	for _, signum := range []string{"parity", "reach"} {
		assert.Contains(t, ids, signum, "ROOT's extended card lacks "+signum+", which the public card does not show")
	}
	assert.NotContains(t, ids, amSignumName, "am is the card, not a skill on it")
}

// A node whose am.toml names it nothing has no card to give, and says so with
// what is lacking (§3.1.11 ExtendedAgentCardNotConfiguredError).
func TestAnExtendedCardThatLacksAFieldIsNotConfigured(t *testing.T) {
	srv, tokens := pluginServingServer(t, "fake")

	w := askingTheExtendedCard(srv, "/a2a/extendedAgentCard", tokens[auth.LevelRoot])
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "EXTENDED_AGENT_CARD_NOT_CONFIGURED")
	assert.Contains(t, w.Body.String(), "AgentCard.name")
}

// The node serves no agent at a tenant, wherever the tenant arrived: in the
// path, or in the query of the route without one (§3.3.2, §8.3.2).
func TestNoAgentIsServedAtATenantNamedAnywhere(t *testing.T) {
	namedNode(t)
	srv, tokens := pluginServingServer(t, "fake")

	for _, path := range []string{"/a2a/garden/extendedAgentCard", "/a2a/extendedAgentCard?tenant=garden"} {
		w := askingTheExtendedCard(srv, path, tokens[auth.LevelRoot])
		assert.Equal(t, http.StatusNotFound, w.Code, path+": "+w.Body.String())
		assert.NotContains(t, w.Body.String(), "\"name\"", path+" was answered a card")
	}
}

// The binding is reached through the gate, and a caller it did not admit is
// refused before any card is built (§13.3: it "MUST require authentication").
func TestTheExtendedCardRequiresAnAdmittedCaller(t *testing.T) {
	namedNode(t)
	srv, _ := pluginServingServer(t, "fake")

	_, refused := srv.extendedCard(httptest.NewRequest(http.MethodGet, "/a2a/extendedAgentCard", nil))
	require.NotNil(t, refused)
	assert.Contains(t, refused.Message, "no gate")
}
