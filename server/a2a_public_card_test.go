package server

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/access"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/nodedid"
	"github.com/teranos/QNTX/server/a2a"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// keyedNode is a node holding its own key, as a started node does, which its
// public card is signed with.
func keyedNode(srv *QNTXServer) ed25519.PublicKey {
	key := ed25519.NewKeyFromSeed(append(make([]byte, ed25519.SeedSize-1), 7))
	public := key.Public().(ed25519.PublicKey)
	srv.nodeDID = &nodedid.Handler{DID: access.EncodeDIDKey(public), PublicKey: public, PrivateKey: key}
	return public
}

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
	keyedNode(srv)

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
	keyedNode(srv)

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
	keyedNode(srv)

	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, httptest.NewRequest(http.MethodGet, agentCardPath, nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "AgentCard.name")
	assert.Contains(t, w.Body.String(), "AgentCard.description")
}

// The card is signed with the node's key (§8.4): EdDSA, under the key id the
// node's DID document names, over the card as served without its
// signatures. Whoever holds the node's did:key verifies it, and a card changed
// on the way does not verify.
func TestTheAgentCardIsSignedByTheNode(t *testing.T) {
	namedNode(t)
	srv, _ := pluginServingServer(t, "fake")
	public := keyedNode(srv)

	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, httptest.NewRequest(http.MethodGet, agentCardPath, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	built, err := srv.publicCard(httptest.NewRequest(http.MethodGet, agentCardPath, nil)).Message()
	require.NoError(t, err)
	served := dynamicpb.NewMessage(built.Descriptor())
	require.NoError(t, protojson.Unmarshal(w.Body.Bytes(), served))
	require.NoError(t, a2a.Verify(served, public, srv.nodeDID.KeyID()), "the served card does not verify against the node's key")

	var card struct {
		Signatures []struct {
			Protected string `json:"protected"`
		} `json:"signatures"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &card))
	require.Len(t, card.Signatures, 1)
	header, err := base64.RawURLEncoding.DecodeString(card.Signatures[0].Protected)
	require.NoError(t, err)
	assert.JSONEq(t, `{"alg":"EdDSA","typ":"JOSE","kid":"`+srv.nodeDID.KeyID()+`"}`, string(header))
	assert.Equal(t, srv.nodeDID.DID+"#"+srv.nodeDID.DID[len("did:key:"):], srv.nodeDID.KeyID(), "the key id is not the one the DID document names")

	served.Set(served.Descriptor().Fields().ByName("description"), protoreflect.ValueOfString("Somebody else."))
	assert.Error(t, a2a.Verify(served, public, srv.nodeDID.KeyID()), "a card changed on the way still verifies")
}

// A node holding no key has nothing to sign its card with, and does not serve
// it unsigned.
func TestAnAgentCardTheNodeCannotSignIsRefused(t *testing.T) {
	namedNode(t)
	srv, _ := pluginServingServer(t, "fake")

	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, httptest.NewRequest(http.MethodGet, agentCardPath, nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "holds none")
}
