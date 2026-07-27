package auth

import (
	"encoding/json"
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
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Label) == "" {
		writeError(w, http.StatusBadRequest, "label is required")
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

	raw, id, err := h.tokens.Create(req.Label, expiresAt)
	if err != nil {
		h.logger.Errorw("failed to create access token", "label", req.Label, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}
	resp := map[string]any{
		"id":         id,
		"label":      req.Label,
		"token":      raw,
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
