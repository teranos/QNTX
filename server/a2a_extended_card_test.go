package server

import (
	"context"
	"encoding/json"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/server/auth"
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

// GetExtendedAgentCard is the card its caller reaches: a skill per signum
// ROOT's lines reach over A2A, signed with the node's key. It is the card am
// node shows.
func TestTheExtendedCardIsWhatTheCallerReaches(t *testing.T) {
	namedNode(t)
	srv, tokens := pluginServingServer(t, "fake")
	keyedNode(srv)

	w := askingTheExtendedCard(srv, "/a2a/extendedAgentCard", tokens[auth.LevelRoot])
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	served := &a2asdk.AgentCard{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), served))
	require.NoError(t, verifiedBy(context.Background(), served, srv.nodeDID.PublicKey, srv.nodeDID.KeyID()), "the extended card is not signed by the node")

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

// The node serves no agent at a tenant no namespace opted into (§3.3.2).
func TestNoAgentIsServedAtATenantNamedAnywhere(t *testing.T) {
	namedNode(t)
	srv, tokens := pluginServingServer(t, "fake")

	for _, path := range []string{"/a2a/garden/extendedAgentCard"} {
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

	r := httptest.NewRequest(http.MethodGet, "/a2a/extendedAgentCard", nil)
	_, refused := srv.a2aExtendedCardOf(context.WithValue(r.Context(), a2aExchangeKey{}, &a2aExchange{request: r}), &a2asdk.GetExtendedAgentCardRequest{})
	require.NotNil(t, refused)
	assert.Contains(t, refused.Error(), "no gate")
}
