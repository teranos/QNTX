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
	// Both grants, because a client reads this to learn what the node does and
	// one left out is a grant nothing will ever ask for.
	assert.Equal(t, []string{"authorization_code", "refresh_token"}, doc.GrantTypesSupported)
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

// An MCP client that has never seen this node starts at a 401 and reads where
// to authenticate off it (RFC 9728 §5.1). A redirect to the login page is a
// page for a person, and the client cannot read it.
func TestAnUnauthenticatedMCPCallIsToldWhereToAuthenticate(t *testing.T) {
	h, _, _ := authorizingHandler(t)
	for _, path := range []string{"/mcp", "/mcp/"} {
		guarded := h.Middleware(path, Also(LevelMCP), func(http.ResponseWriter, *http.Request) {})

		w := httptest.NewRecorder()
		guarded(w, httptest.NewRequest(http.MethodPost, path, nil))

		require.Equal(t, http.StatusUnauthorized, w.Code, path+": "+w.Body.String())
		assert.Equal(t, `Bearer resource_metadata="`+nodeOrigin+protectedResourcePath+`"`,
			w.Header().Get("WWW-Authenticate"), path)
	}
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
