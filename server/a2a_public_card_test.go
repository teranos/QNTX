package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/server/a2a"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/dynamicpb"
)

// namedNode gives the node a name and a description for one test.
func namedNode(t *testing.T) {
	t.Helper()
	appcfg.Set("node.name", "QNTX")
	appcfg.Set("node.description", "QNTX implements Continuous Intelligence. Systems that continuously evolve their understanding through verifiable attestations.")
	t.Cleanup(func() {
		appcfg.Set("node.name", "")
		appcfg.Set("node.description", "")
	})
}

// A stranger is served the card whole: an AgentCard lacking nothing the spec
// requires, the signa named for it in their order, each requiring the bearer
// token, and the node extension.
func TestTheAgentCardIsServedToAnyone(t *testing.T) {
	namedNode(t)
	srv, _ := pluginServingServer(t, "fake")

	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, httptest.NewRequest(http.MethodGet, agentCardPath, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Equal(t, "public, max-age=60", w.Header().Get("Cache-Control"))
	assert.NotEmpty(t, w.Header().Get("ETag"))

	built, err := srv.publicCard(httptest.NewRequest(http.MethodGet, agentCardPath, nil)).Message()
	require.NoError(t, err)
	served := dynamicpb.NewMessage(built.Descriptor())
	require.NoError(t, protojson.Unmarshal(w.Body.Bytes(), served), "what was served is not an "+a2a.AgentCard)
	assert.Empty(t, a2a.Missing(served))

	var card struct {
		Name         string   `json:"name"`
		InputModes   []string `json:"defaultInputModes"`
		OutputModes  []string `json:"defaultOutputModes"`
		Capabilities struct {
			Extensions []struct {
				URI    string         `json:"uri"`
				Params map[string]any `json:"params"`
			} `json:"extensions"`
		} `json:"capabilities"`
		Skills []struct {
			ID       string `json:"id"`
			Requires []any  `json:"securityRequirements"`
		} `json:"skills"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &card))
	assert.Equal(t, "QNTX", card.Name)
	assert.Equal(t, a2a.Modes, card.InputModes)
	assert.Equal(t, a2a.Modes, card.OutputModes)
	var ids []string
	for _, skill := range card.Skills {
		ids = append(ids, skill.ID)
		assert.NotEmpty(t, skill.Requires, skill.ID+" does not say it takes the bearer token")
	}
	assert.Equal(t, agentCardSigna, ids)
	require.Len(t, card.Capabilities.Extensions, 1)
	assert.Equal(t, nodeExtensionURI, card.Capabilities.Extensions[0].URI)
	assert.Contains(t, card.Capabilities.Extensions[0].Params, "syscap")
	assert.Contains(t, card.Capabilities.Extensions[0].Params, "health")
}

// Asked again with the ETag it was given, the card is not sent again.
func TestTheAgentCardAnswersItsETag(t *testing.T) {
	namedNode(t)
	srv, _ := pluginServingServer(t, "fake")

	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, httptest.NewRequest(http.MethodGet, agentCardPath, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	again := httptest.NewRequest(http.MethodGet, agentCardPath, nil)
	again.Header.Set("If-None-Match", w.Header().Get("ETag"))
	w = httptest.NewRecorder()
	srv.served.ServeHTTP(w, again)
	assert.Equal(t, http.StatusNotModified, w.Code)
	assert.Empty(t, w.Body.String())
}

// With no name in am.toml the card lacks a field the spec requires, and is
// refused, naming it.
func TestAnAgentCardThatLacksAFieldIsRefused(t *testing.T) {
	srv, _ := pluginServingServer(t, "fake")

	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, httptest.NewRequest(http.MethodGet, agentCardPath, nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "AgentCard.name")
	assert.Contains(t, w.Body.String(), "AgentCard.description")
}
