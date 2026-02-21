package auth

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const ownerUserID = "qntx-owner"
const ownerUserName = "owner"

// ownerUser implements webauthn.User for the single QNTX owner
type ownerUser struct {
	credentials []webauthn.Credential
}

func (u *ownerUser) WebAuthnID() []byte                         { return []byte(ownerUserID) }
func (u *ownerUser) WebAuthnName() string                       { return ownerUserName }
func (u *ownerUser) WebAuthnDisplayName() string                { return "QNTX Owner" }
func (u *ownerUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(loginHTML)
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	registered, err := h.creds.exists()
	if err != nil {
		h.logger.Errorw("Failed to check credential status", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to check credential status")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"registered": registered})
}

func (h *Handler) handleRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	registered, err := h.creds.exists()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check credentials")
		return
	}
	if registered {
		writeError(w, http.StatusConflict, "credential already registered")
		return
	}

	user := &ownerUser{}
	options, session, err := h.webauthn.BeginRegistration(user,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementDiscouraged),
	)
	if err != nil {
		h.logger.Errorw("WebAuthn BeginRegistration failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to begin registration")
		return
	}

	h.ceremonies.Store(ownerUserID, session)
	writeJSON(w, http.StatusOK, options)
}

func (h *Handler) handleRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionVal, ok := h.ceremonies.LoadAndDelete(ownerUserID)
	if !ok {
		writeError(w, http.StatusBadRequest, "no registration ceremony in progress")
		return
	}
	session := sessionVal.(*webauthn.SessionData)

	user := &ownerUser{}
	credential, err := h.webauthn.FinishRegistration(user, *session, r)
	if err != nil {
		h.logger.Errorw("WebAuthn FinishRegistration failed", "error", err)
		writeError(w, http.StatusBadRequest, fmt.Sprintf("registration failed: %v", err))
		return
	}

	if len(credential.ID) > 1024 || len(credential.PublicKey) > 4096 {
		writeError(w, http.StatusBadRequest, "credential too large")
		return
	}

	if err := h.creds.save(*credential); err != nil {
		h.logger.Errorw("Failed to save credential", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save credential")
		return
	}

	token, err := h.sessions.create()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	setSessionCookie(w, token)

	h.logger.Infow("WebAuthn credential registered and session created")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleLoginBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	creds, err := h.creds.getAll()
	if err != nil || len(creds) == 0 {
		writeError(w, http.StatusBadRequest, "no credentials registered")
		return
	}

	user := &ownerUser{credentials: creds}
	options, session, err := h.webauthn.BeginLogin(user)
	if err != nil {
		h.logger.Errorw("WebAuthn BeginLogin failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to begin login")
		return
	}

	h.ceremonies.Store(ownerUserID, session)
	writeJSON(w, http.StatusOK, options)
}

func (h *Handler) handleLoginFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	sessionVal, ok := h.ceremonies.LoadAndDelete(ownerUserID)
	if !ok {
		writeError(w, http.StatusBadRequest, "no login ceremony in progress")
		return
	}
	session := sessionVal.(*webauthn.SessionData)

	creds, err := h.creds.getAll()
	if err != nil || len(creds) == 0 {
		writeError(w, http.StatusBadRequest, "no credentials registered")
		return
	}

	user := &ownerUser{credentials: creds}
	credential, err := h.webauthn.FinishLogin(user, *session, r)
	if err != nil {
		h.logger.Errorw("WebAuthn FinishLogin failed", "error", err)
		writeError(w, http.StatusUnauthorized, "authentication failed")
		return
	}

	if err := h.creds.updateSignCount(credential.ID, credential.Authenticator.SignCount); err != nil {
		h.logger.Warnw("Failed to update sign count", "error", err)
	}

	token, err := h.sessions.create()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	setSessionCookie(w, token)

	h.logger.Infow("WebAuthn authentication successful")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		h.sessions.invalidate(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Response helpers (package-local) ---

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}
