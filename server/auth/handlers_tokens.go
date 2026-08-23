package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// handleCreateToken issues a new access token for the calling passkey session.
// POST /auth/tokens
// Body: {"label": "<name>", "expires_at": "<RFC3339>?"}
// Response: {"id","label","token","created_at","expires_at"} — token is the
// raw value, returned exactly once.
func (h *Handler) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.tokens == nil {
		writeError(w, http.StatusServiceUnavailable, "token store not configured")
		return
	}

	var req struct {
		Label     string  `json:"label"`
		ExpiresAt *string `json:"expires_at,omitempty"`
		Namespace string  `json:"namespace,omitempty"`
		Scope     struct {
			Read  []string `json:"read"`
			Write []string `json:"write"`
		} `json:"scope"`
	}
	// Bounded like every other body in this package. A session holder is not a
	// stranger, but a label is a string and nothing capped how long.
	if err := json.NewDecoder(io.LimitReader(r.Body, maxCeremonyBodyBytes)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body, or larger than 256 KiB")
		return
	}
	if strings.TrimSpace(req.Label) == "" {
		writeError(w, http.StatusBadRequest, "label is required")
		return
	}
	if len(req.Scope.Read) == 0 && len(req.Scope.Write) == 0 {
		writeError(w, http.StatusBadRequest,
			`scope.read or scope.write must name at least one predicate, or "*" for every predicate; a token with neither can do nothing`)
		return
	}

	// The session that asked is who the token speaks for. sessionOnly already
	// ran, so this is present.
	p := h.presented(r)
	mintedBy, _ := p.Enrolling()

	namespace := req.Namespace
	if namespace == "" {
		namespace = NamespaceDefault
	}
	if namespace == NamespaceSystem {
		writeError(w, http.StatusForbidden, "no token acts in the system namespace")
		return
	}
	// Naming a namespace is crossing into one, which ADR-027 puts at SUPER.
	// am.toml is the only list of who that is, so being on it is the check —
	// a deployment naming nobody mints into default and no further.
	if namespace != NamespaceDefault && !h.stillAdmitted(mintedBy) {
		h.logger.Infow("token mint refused: naming a namespace is not this caller's to do",
			"namespace", namespace, "minted_by", mintedBy)
		writeError(w, http.StatusForbidden,
			"naming a namespace needs an identity listed in auth.root_identities; this session is "+
				quoteIdentity(mintedBy))
		return
	}
	// A node opens one attestation store and pins it to default (ADR-026), so
	// a token naming another namespace is refused on every use. Minting it
	// anyway is the reporting-success failure one step earlier.
	if namespace != NamespaceDefault {
		h.logger.Infow("token mint refused: the node serves one namespace",
			"namespace", namespace, "serves", NamespaceDefault)
		writeError(w, http.StatusConflict,
			"this node reads and writes the "+NamespaceDefault+" namespace only, so a token for "+
				namespace+" could not be used; nothing routes a caller to another namespace yet (ADR-026)")
		return
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			writeError(w, http.StatusBadRequest, "expires_at must be RFC3339")
			return
		}
		expiresAt = &t
	}

	// The minting session already carries who it belongs to, so recording the
	// person costs nothing and no later use has to look them up.
	mintedByUser, mintedByDisplayName := p.UserID, p.DisplayName

	raw, id, err := h.tokens.Create(NewToken{
		Label:               req.Label,
		ExpiresAt:           expiresAt,
		MintedBy:            mintedBy,
		MintedByUser:        mintedByUser,
		MintedByDisplayName: mintedByDisplayName,
		Namespace:           namespace,
		ScopeRead:           req.Scope.Read,
		ScopeWrite:          req.Scope.Write,
	})
	if err != nil {
		h.logger.Errorw("failed to create access token", "label", req.Label,
			"namespace", namespace, "minted_by", mintedBy, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}
	h.logger.Infow("access token minted", "id", id, "label", req.Label,
		"namespace", namespace, "minted_by", mintedBy,
		"scope_read", req.Scope.Read, "scope_write", req.Scope.Write)
	resp := map[string]any{
		"id":         id,
		"label":      req.Label,
		"token":      raw,
		"minted_by":  mintedBy,
		"namespace":  namespace,
		"created_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if expiresAt != nil {
		resp["expires_at"] = expiresAt.UTC().Format(time.RFC3339Nano)
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleListTokens returns all tokens minus raw values and hashes.
// GET /auth/tokens
func (h *Handler) handleListTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.tokens == nil {
		writeError(w, http.StatusServiceUnavailable, "token store not configured")
		return
	}
	infos, err := h.tokens.List()
	if err != nil {
		h.logger.Errorw("failed to list access tokens", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list tokens")
		return
	}
	if infos == nil {
		infos = []TokenInfo{}
	}
	writeJSON(w, http.StatusOK, infos)
}

// handleTokenByID routes the operations that name one token.
//
//	DELETE /auth/tokens/{id}          revoke
//	POST   /auth/tokens/{id}/enable   lift the revocation
//
// Revocation is a switch (ADR-025): kill the token, watch whether anything is
// still presenting it, turn it back on if that was you.
func (h *Handler) handleTokenByID(w http.ResponseWriter, r *http.Request) {
	if h.tokens == nil {
		writeError(w, http.StatusServiceUnavailable, "token store not configured")
		return
	}
	const prefix = "/auth/tokens/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		writeError(w, http.StatusBadRequest, "malformed path")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, prefix)

	if id, ok := strings.CutSuffix(rest, "/enable"); ok {
		h.handleEnableToken(w, r, id)
		return
	}
	h.handleRevokeToken(w, r, rest)
}

// handleRevokeToken stops a token authenticating. DELETE /auth/tokens/{id}
func (h *Handler) handleRevokeToken(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	if err := h.tokens.Revoke(id); err != nil {
		h.logger.Errorw("failed to revoke access token", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to revoke token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "revoked", "id": id})
}

// handleEnableToken lifts a revocation. POST /auth/tokens/{id}/enable
//
// It does not extend an expiry — a token past its expiry stays dead whatever
// this returns.
func (h *Handler) handleEnableToken(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if id == "" {
		writeError(w, http.StatusBadRequest, "id is required")
		return
	}
	if err := h.tokens.Enable(id); err != nil {
		h.logger.Errorw("failed to enable access token", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to enable token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "enabled", "id": id})
}
