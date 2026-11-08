package server

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	a2asdk "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/teranos/QNTX/internal/access"
	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/QNTX/internal/nodedid"
	"github.com/teranos/QNTX/server/auth"
)

// keyedNode is a node holding its own key, as a started node does, which its
// public card is signed with.
func keyedNode(srv *QNTXServer) ed25519.PublicKey {
	key := ed25519.NewKeyFromSeed(append(make([]byte, ed25519.SeedSize-1), 7))
	public := key.Public().(ed25519.PublicKey)
	srv.nodeDID = &nodedid.Handler{DID: access.EncodeDIDKey(public), PublicKey: public, PrivateKey: key}
	// 「[Opus 5.5] The node signs as this key wherever it signs, carrying a person to a peer too.」
	if srv.authHandler != nil {
		srv.authHandler.SetNodeKey(key)
	}
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

// A stranger is served the card whole: the signa named for it in their
// order, each requiring the bearer token, and the node extension.
func TestTheAgentCardIsServedToAnyone(t *testing.T) {
	namedNode(t)
	srv, _ := pluginServingServer(t, "fake")
	keyedNode(srv)

	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, httptest.NewRequest(http.MethodGet, agentCardPath, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

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
	assert.Equal(t, cardModes, card.InputModes)
	assert.Equal(t, cardModes, card.OutputModes)
	var ids []string
	for _, skill := range card.Skills {
		ids = append(ids, skill.ID)
		assert.NotEmpty(t, skill.Requires, skill.ID+" does not say it takes the bearer token")
	}
	assert.Equal(t, agentCardSigna, ids)
	// The node admits a person's passkey session as it admits the bearer token,
	// so both are schemes it declares, and either one is enough (§13.3).
	var security struct {
		Schemes map[string]struct {
			APIKey *struct {
				Location string `json:"location"`
				Name     string `json:"name"`
			} `json:"apiKeySecurityScheme"`
			HTTP *struct {
				Scheme string `json:"scheme"`
			} `json:"httpAuthSecurityScheme"`
		} `json:"securitySchemes"`
		Required []struct {
			Schemes map[string]any `json:"schemes"`
		} `json:"securityRequirements"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &security))
	require.NotNil(t, security.Schemes["bearer"].HTTP)
	assert.Equal(t, "Bearer", security.Schemes["bearer"].HTTP.Scheme)
	require.NotNil(t, security.Schemes["session"].APIKey)
	assert.Equal(t, "cookie", security.Schemes["session"].APIKey.Location)
	assert.Equal(t, auth.SessionCookie, security.Schemes["session"].APIKey.Name)
	var alternatives []string
	for _, required := range security.Required {
		require.Len(t, required.Schemes, 1, "a requirement naming two schemes asks for both at once")
		for scheme := range required.Schemes {
			alternatives = append(alternatives, scheme)
		}
	}
	assert.ElementsMatch(t, []string{"bearer", "session"}, alternatives)

	require.Len(t, card.Capabilities.Extensions, 1)
	assert.Equal(t, nodeExtensionURI, card.Capabilities.Extensions[0].URI)
	assert.Contains(t, card.Capabilities.Extensions[0].Params, "syscap")
	assert.Contains(t, card.Capabilities.Extensions[0].Params, "health")
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

	served := &a2asdk.AgentCard{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), served))
	require.NoError(t, verifiedBy(context.Background(), w.Body.Bytes(), public, srv.nodeDID.KeyID()), "the served card does not verify against the node's key")

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

	served.Description = "Somebody else."
	changed, err := json.Marshal(served)
	require.NoError(t, err)
	assert.Error(t, verifiedBy(context.Background(), changed, public, srv.nodeDID.KeyID()), "a card changed on the way still verifies")
}

// A node holding no key has nothing to sign its card with, and does not serve
// it unsigned.
func TestAnAgentCardTheNodeCannotSignIsRefused(t *testing.T) {
	namedNode(t)
	srv, _ := pluginServingServer(t, "fake")

	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, httptest.NewRequest(http.MethodGet, agentCardPath, nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// The card says how it is cached: a short max-age, as health is on it, and a
// strong ETag a client sends back to be told it holds the card it would get.
func TestTheAgentCardSaysHowItIsCached(t *testing.T) {
	namedNode(t)
	srv, _ := pluginServingServer(t, "fake")
	keyedNode(srv)

	w := httptest.NewRecorder()
	srv.served.ServeHTTP(w, httptest.NewRequest(http.MethodGet, agentCardPath, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "public, max-age=60", w.Header().Get("Cache-Control"))
	etag := w.Header().Get("ETag")
	require.NotEmpty(t, etag)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	again := httptest.NewRequest(http.MethodGet, agentCardPath, nil)
	again.Header.Set("If-None-Match", etag)
	w = httptest.NewRecorder()
	srv.served.ServeHTTP(w, again)
	assert.Equal(t, http.StatusNotModified, w.Code)
	assert.Empty(t, w.Body.String())

	stale := httptest.NewRequest(http.MethodGet, agentCardPath, nil)
	stale.Header.Set("If-None-Match", `"not-this-card"`)
	w = httptest.NewRecorder()
	srv.served.ServeHTTP(w, stale)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, w.Body.String())
}
