package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// handleCreateToken issues a new access token for the calling passkey session.
// POST /auth/tokens
// Body: {"label": "<name>", "expires_at": "<RFC3339>?"}
// Response: {"id","label","token","created_at","expires_at"} — token is the
// raw value, returned exactly once.
func (h *Handler) handleCreateToken(w http.ResponseWriter, r *http.Request, p Presented) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.tokens == nil {
		h.writeError(w, http.StatusServiceUnavailable, "token store not configured")
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
	// MaxBytesReader rather than LimitReader, so being too large and not being
	// JSON stay two answers instead of one.
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCeremonyBodyBytes)).Decode(&req); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			h.writeError(w, http.StatusRequestEntityTooLarge, "the body is larger than 256 KiB")
			return
		}
		h.writeError(w, http.StatusBadRequest, "the body did not parse as JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Label) == "" {
		h.writeError(w, http.StatusBadRequest, "no label")
		return
	}

	// The session that asked is who the token speaks for, and sessionOnly
	// resolved it — asking the request again could answer differently.

	// A session and only a session: a half-admission has no device behind it
	// and must never name the minter of something that outlives the session.
	mintedBy, _ := p.Admitted()

	namespace := req.Namespace
	if namespace == "" {
		namespace = NamespaceDefault
	}
	// A root identity mints with its own reach, and its own reach includes the
	// system namespace. Nobody else gets there.
	if namespace == NamespaceSystem && !h.stillAdmitted(mintedBy) {
		h.writeError(w, http.StatusForbidden, notListed(mintedBy))
		return
	}
	// Naming a namespace is crossing into one, which ADR-027 puts at SUPER.
	// am.toml is the only list of who that is, so being on it is the check.
	if namespace != NamespaceDefault && !h.stillAdmitted(mintedBy) {
		h.writeError(w, http.StatusForbidden, notListed(mintedBy))
		return
	}
	// A node opens one attestation store and pins it to default (ADR-026), so
	// a token naming another namespace is refused on every use. Minting it
	// anyway is the reporting-success failure one step earlier.
	if namespace != NamespaceDefault {
		h.writeError(w, http.StatusConflict, "the node does not serve "+namespace)
		return
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		t, err := time.Parse(time.RFC3339, *req.ExpiresAt)
		if err != nil {
			h.writeError(w, http.StatusBadRequest, "expires_at must be RFC3339")
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
		h.attest(PredicateUnanswered, mintedBy, map[string]any{
			"asked": "token store", "doing": "mint", "error": err.Error(),
		})
		// Only a session reaches here, so nothing is withheld.
		h.writeError(w, http.StatusInternalServerError, "the token was not written: "+err.Error())
		return
	}
	// A token outlives the session that minted it, so both ends of its life are
	// a record rather than a log line.
	h.attest(PredicateMinted, mintedBy, map[string]any{
		"token": id, "label": req.Label, "namespace": namespace,
		"scope_read": req.Scope.Read, "scope_write": req.Scope.Write,
	})
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
	h.writeJSON(w, http.StatusOK, resp)
}

// handleListTokens returns all tokens minus raw values and hashes.
// GET /auth/tokens
func (h *Handler) handleListTokens(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.tokens == nil {
		h.writeError(w, http.StatusServiceUnavailable, "token store not configured")
		return
	}
	infos, err := h.tokens.List()
	if err != nil {
		h.logger.Errorw("failed to list access tokens", "error", err)
		h.writeError(w, http.StatusInternalServerError, "the token store did not answer: "+err.Error())
		return
	}
	if infos == nil {
		infos = []TokenInfo{}
	}
	h.writeJSON(w, http.StatusOK, infos)
}

// handleTokenByID routes the operations that name one token.
//
//	DELETE /auth/tokens/{id}          revoke
//	POST   /auth/tokens/{id}/enable   lift the revocation
//
// Revocation is a switch (ADR-025): kill the token, watch whether anything is
// still presenting it, turn it back on if that was you.
func (h *Handler) handleTokenByID(w http.ResponseWriter, r *http.Request, p Presented) {
	if h.tokens == nil {
		h.writeError(w, http.StatusServiceUnavailable, "token store not configured")
		return
	}
	const prefix = "/auth/tokens/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		h.writeError(w, http.StatusBadRequest, "malformed path")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, prefix)

	if id, ok := strings.CutSuffix(rest, "/enable"); ok {
		h.handleEnableToken(w, r, p, id)
		return
	}
	if id, ok := strings.CutSuffix(rest, "/scope"); ok {
		h.handleScopeToken(w, r, p, id)
		return
	}
	h.handleRevokeToken(w, r, p, rest)
}

// handleRevokeToken stops a token authenticating. DELETE /auth/tokens/{id}
func (h *Handler) handleRevokeToken(w http.ResponseWriter, r *http.Request, p Presented, id string) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "no id")
		return
	}
	by, _ := p.Admitted()
	if err := h.tokens.Revoke(id); err != nil {
		h.attest(PredicateUnanswered, by, map[string]any{
			"asked": "token store", "doing": "revoke", "token": id, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "the token was not written: "+err.Error())
		return
	}
	h.attest(PredicateRevoked, by, map[string]any{"token": id})
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "revoked", "id": id})
}

// handleScopeToken replaces what a token may touch (27-1). The scope changes on
// the token that holds it, rather than by minting a second one.
// PUT /auth/tokens/{id}/scope
func (h *Handler) handleScopeToken(w http.ResponseWriter, r *http.Request, p Presented, id string) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "no id")
		return
	}

	// Both lists together: they are one answer to what a token may touch, and
	// sending one alone leaves the other saying what it said before.
	var req struct {
		Read  []string `json:"read"`
		Write []string `json:"write"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	by, _ := p.Admitted()
	if err := h.tokens.SetScope(id, req.Read, req.Write); err != nil {
		h.attest(PredicateUnanswered, by, map[string]any{
			"asked": "token store", "doing": "set scope", "token": id, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "the token was not written: "+err.Error())
		return
	}
	h.attest(PredicateScoped, by, map[string]any{
		"token": id, "scope_read": req.Read, "scope_write": req.Write,
	})
	h.writeJSON(w, http.StatusOK, map[string]any{
		"status": "scoped", "id": id, "read": req.Read, "write": req.Write,
	})
}

// handleEnableToken lifts a revocation. POST /auth/tokens/{id}/enable
//
// It does not extend an expiry — a token past its expiry stays dead whatever
// this returns.
func (h *Handler) handleEnableToken(w http.ResponseWriter, r *http.Request, p Presented, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if id == "" {
		h.writeError(w, http.StatusBadRequest, "no id")
		return
	}
	by, _ := p.Admitted()
	if err := h.tokens.Enable(id); err != nil {
		h.attest(PredicateUnanswered, by, map[string]any{
			"asked": "token store", "doing": "enable", "token": id, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "the token was not written: "+err.Error())
		return
	}
	h.attest(PredicateEnabled, by, map[string]any{"token": id})
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "enabled", "id": id})
}
