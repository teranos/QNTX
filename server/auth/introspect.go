package auth

import (
	"net/http"
	"slices"
	"time"
)

// Introspection (RFC 7662): a node asks this node whether a token it was
// handed is live, and who and where it is. Only a node may ask, signing as
// its did:key, and only about tokens that act where the node was registered.

// introspectPath is where a node asks.
const introspectPath = "/auth/introspect"

// introspection is RFC 7662 §2.2, the members this node has an answer for, and
// what the gate admits the token as.
type introspection struct {
	Active     bool     `json:"active"`
	Issuer     string   `json:"iss,omitempty"`
	Subject    string   `json:"sub,omitempty"`
	ClientID   string   `json:"client_id,omitempty"`
	TokenType  string   `json:"token_type,omitempty"`
	Level      string   `json:"level,omitempty"`
	Namespaces []string `json:"namespaces,omitempty"`
	Label      string   `json:"label,omitempty"`
	DID        string   `json:"did,omitempty"`
	User       string   `json:"user,omitempty"`
	Name       string   `json:"name,omitempty"`
}

// handleIntrospect answers POST /auth/introspect: token, and the asking
// node's client assertion. A token the node may not know of is not active to
// it (§2.2), the same answer as a token nobody issued.
func (h *Handler) handleIntrospect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		h.writeError(w, http.StatusBadRequest, "the form did not read: "+err.Error())
		return
	}
	if r.PostForm.Get("client_assertion_type") != clientAssertionType {
		h.writeError(w, http.StatusUnauthorized, "a node asks by signing as its did:key, and this carried no client assertion")
		return
	}
	asked, err := h.assertedClient(r.PostForm, time.Now())
	if err != nil {
		h.logger.Infow("Introspection refused", "client", r.PostFormValue("client_id"), "error", err)
		h.writeError(w, http.StatusUnauthorized, "the asking node did not prove itself")
		return
	}
	asker, ok := h.clientByDID(asked.GetID())
	if !ok || !asker.isNodeClient() {
		h.writeError(w, http.StatusUnauthorized, "only a node asks about tokens")
		return
	}
	token := r.PostForm.Get("token")
	if token == "" || h.tokens == nil {
		h.writeJSON(w, http.StatusOK, introspection{})
		return
	}
	grant, live := h.tokens.Lookup(sha256Hex(token))
	if !live {
		h.writeJSON(w, http.StatusOK, introspection{})
		return
	}
	admitted, ok := h.admissionOf(Presented{Bearer: &grant, bearerPresented: true})
	if !ok {
		h.writeJSON(w, http.StatusOK, introspection{})
		return
	}
	// No namespace is every namespace, by level (ADR-027); otherwise it has to be the asker's.
	if len(admitted.Namespaces) > 0 && !slices.Contains(admitted.Namespaces, asker.Namespace) {
		h.logger.Infow("Introspection answered inactive",
			"asker", asker.DID, "asker_namespace", asker.Namespace, "token_namespaces", admitted.Namespaces)
		h.writeJSON(w, http.StatusOK, introspection{})
		return
	}
	h.writeJSON(w, http.StatusOK, introspection{
		Active:     true,
		Issuer:     h.publicOrigin(),
		Subject:    admitted.Identity,
		ClientID:   grant.ClientDID,
		TokenType:  "Bearer",
		Level:      admitted.LevelName(),
		Namespaces: admitted.Namespaces,
		Label:      grant.Label,
		DID:        grant.DID,
		User:       admitted.UserID,
		Name:       admitted.DisplayName,
	})
}
