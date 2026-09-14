package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Discovery documents: a client that has never seen this node finds its
// doors here, and what it finds is what the branch built.

func TestTheAuthorizationServerDocumentNamesTheDoors(t *testing.T) {
	h, _, _ := authorizingHandler(t)

	w := httptest.NewRecorder()
	h.handleAuthorizationServerMetadata(w, httptest.NewRequest(http.MethodGet, authorizationServerPath, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var doc authorizationServerMetadata
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &doc))
	assert.Equal(t, nodeOrigin, doc.Issuer)
	assert.Equal(t, nodeOrigin+"/auth/authorize", doc.AuthorizationEndpoint)
	assert.Equal(t, nodeOrigin+"/auth/token", doc.TokenEndpoint)
	assert.Equal(t, []string{"code"}, doc.ResponseTypesSupported)
	assert.Equal(t, []string{"authorization_code"}, doc.GrantTypesSupported)
	assert.Equal(t, []string{"S256"}, doc.CodeChallengeMethodsSupported)
	assert.ElementsMatch(t, []string{"client_secret_basic", "client_secret_post"}, doc.TokenEndpointAuthMethodsSupported)
}

func TestTheProtectedResourceDocumentNamesThisNode(t *testing.T) {
	h, _, _ := authorizingHandler(t)

	w := httptest.NewRecorder()
	h.handleProtectedResourceMetadata(w, httptest.NewRequest(http.MethodGet, protectedResourcePath, nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var doc protectedResourceMetadata
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &doc))
	assert.Equal(t, nodeOrigin, doc.Resource)
	assert.Equal(t, []string{nodeOrigin}, doc.AuthorizationServers)
	assert.Equal(t, []string{"header"}, doc.BearerMethodsSupported)
}

// The documents are read, never written.
func TestTheDiscoveryDocumentsAnswerOnlyAGet(t *testing.T) {
	h, _, _ := authorizingHandler(t)
	for path, handler := range map[string]http.HandlerFunc{
		authorizationServerPath: h.handleAuthorizationServerMetadata,
		protectedResourcePath:   h.handleProtectedResourceMetadata,
	} {
		w := httptest.NewRecorder()
		handler(w, httptest.NewRequest(http.MethodPost, path, nil))
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code, path)
	}
}
