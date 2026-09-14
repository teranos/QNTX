package auth

import "net/http"

// Discovery documents: two JSON files at fixed paths that a client reads
// before it sends anybody anywhere. They are how a client that has never
// seen this node finds its doors without being configured by hand. Both say
// only what the branch already built; nothing here is a second source of
// what the node serves.

// authorizationServerPath is RFC 8414's path for the authorization server's
// metadata, at the root because the issuer has no path (§3).
const authorizationServerPath = "/.well-known/oauth-authorization-server"

// protectedResourcePath is RFC 9728's path for the resource's metadata: this
// node, and which authorization server issues tokens for it, which is this
// node.
const protectedResourcePath = "/.well-known/oauth-protected-resource"

// authorizationServerMetadata is RFC 8414 §2, the members this node has an
// answer for.
type authorizationServerMetadata struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	GrantTypesSupported               []string `json:"grant_types_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
	CodeChallengeMethodsSupported     []string `json:"code_challenge_methods_supported"`
}

// protectedResourceMetadata is RFC 9728 §2, the members this node has an
// answer for.
type protectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

// handleAuthorizationServerMetadata is where a client finds the doors. The
// issuer is the node's public origin; the authorize and token endpoints are
// the ones under it; a code is the one response, exchanged with S256 PKCE by
// a client presenting its secret as Basic auth or in the form.
func (h *Handler) handleAuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	issuer := h.publicOrigin()
	h.writeJSON(w, http.StatusOK, authorizationServerMetadata{
		Issuer:                            issuer,
		AuthorizationEndpoint:             issuer + authorizePath,
		TokenEndpoint:                     issuer + tokenPath,
		ResponseTypesSupported:            []string{"code"},
		GrantTypesSupported:               []string{"authorization_code"},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_basic", "client_secret_post"},
		CodeChallengeMethodsSupported:     []string{"S256"},
	})
}

// handleProtectedResourceMetadata is where a client that hit this node finds
// who issues tokens for it: this node. A bearer is presented in the header.
func (h *Handler) handleProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	origin := h.publicOrigin()
	h.writeJSON(w, http.StatusOK, protectedResourceMetadata{
		Resource:               origin,
		AuthorizationServers:   []string{origin},
		BearerMethodsSupported: []string{"header"},
	})
}
