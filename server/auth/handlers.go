package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

const ownerUserID = "qntx-owner"
const ownerUserName = "owner"

// maxCeremonyBodyBytes bounds a ceremony response. An attestation object plus
// a DID proof is kilobytes; anything past this is not a ceremony.
const maxCeremonyBodyBytes = 256 << 10

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

	// Empty means a passkey exists but no identity was established — the
	// pre-#577 state. Reporting it lets the UI say so instead of implying one.
	ownerDID, err := h.creds.owner()
	if err != nil {
		h.logger.Errorw("Failed to read the registered owner", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to check credential status")
		return
	}

	// Public keys, and laye needs them to know whose signature on a binding
	// counts. Without the list the browser believes any peer that signs its
	// own claim, which is the Go path's binding_signers check going missing.
	signers := h.identities.trustedSigners()
	if signers == nil {
		signers = []string{}
	}

	// Who this session is, to the session that holds it. A refresh loses what
	// login returned, and without this the browser cannot say which of the
	// identities it holds is the one am.toml admitted.
	identity := ""
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if who, ok := h.sessions.identityOf(cookie.Value); ok {
			identity = who
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"registered":      registered,
		"owner_did":       ownerDID,
		"binding_signers": signers,
		"identity":        identity,
	})
}

func (h *Handler) handleRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := h.mayRegister(r); err != nil {
		h.logger.Warnw("Passkey enrolment refused", "error", err)
		writeError(w, http.StatusUnauthorized, err.Error())
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

	// The body carries both the WebAuthn response and the user DID proof, and
	// the library consumes the request, so read it once and parse it twice.
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCeremonyBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read the registration response")
		return
	}

	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(body))
	if err != nil {
		h.logger.Errorw("WebAuthn registration response did not parse", "error", err)
		writeError(w, http.StatusBadRequest, fmt.Sprintf("registration failed: %v", err))
		return
	}

	user := &ownerUser{}
	credential, err := h.webauthn.CreateCredential(user, *session, parsed)
	if err != nil {
		h.logger.Errorw("WebAuthn FinishRegistration failed", "error", err)
		writeError(w, http.StatusBadRequest, fmt.Sprintf("registration failed: %v", err))
		return
	}

	if len(credential.ID) > 1024 || len(credential.PublicKey) > 4096 {
		writeError(w, http.StatusBadRequest, "credential too large")
		return
	}

	// #577: the browser derives this from the WebAuthn PRF output and signs
	// the ceremony challenge with it.
	ownerDID, err := verifiedOwnerDID(body, session.Challenge)
	if err != nil {
		h.logger.Errorw("User DID proof rejected", "error", err)
		writeError(w, http.StatusBadRequest, "user identity proof rejected")
		return
	}
	// An authenticator that will not say which key it is has no provenance.
	if ownerDID == "" {
		h.logger.Warnw("Passkey enrolment refused: the browser proved no owner key",
			"reason", "WebAuthn PRF produced nothing")
		writeError(w, http.StatusBadRequest, "this browser cannot enrol a passkey here")
		return
	}

	// The session that authorized this enrolment says which account the new
	// passkey speaks for. Unconditional: a deployment listing nobody has
	// nobody to enrol on behalf of.
	admittedAs := h.enrollingIdentity(r)
	if admittedAs == "" {
		h.logger.Warnw("Passkey enrolment refused: the session names no identity",
			"root_identities", len(h.identities.roots()))
		writeError(w, http.StatusForbidden, "sign in before enrolling a passkey")
		return
	}

	if err := h.creds.save(*credential, ownerDID, admittedAs); err != nil {
		h.logger.Errorw("Failed to save credential", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save credential")
		return
	}

	// The half-admission is spent here, so one laye signature buys one device.
	h.pendingLogins.close(heldPending(r))
	h.clearPendingCookie(w)

	token, err := h.sessions.create(admittedAs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	h.setSessionCookie(w, token)

	h.attest(PredicateLoggedIn, admittedAs, map[string]any{
		"provider": "passkey",
		"device":   "enrolled",
		"owner":    ownerDID,
	})
	h.logger.Infow("WebAuthn credential registered and session created", "admitted_as", admittedAs)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleLoginBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// A passkey is the second half of an admission, never the whole of one.
	if _, ok := h.pendingLogins.peek(heldPending(r)); !ok {
		writeError(w, http.StatusForbidden, "sign in first")
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

	if _, ok := h.pendingLogins.peek(heldPending(r)); !ok {
		writeError(w, http.StatusForbidden, "sign in first")
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

	body, err := io.ReadAll(io.LimitReader(r.Body, maxCeremonyBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read the login response")
		return
	}

	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(body))
	if err != nil {
		h.logger.Errorw("WebAuthn login response did not parse", "error", err)
		writeError(w, http.StatusUnauthorized, "authentication failed")
		return
	}

	user := &ownerUser{credentials: creds}
	credential, err := h.webauthn.ValidateLogin(user, *session, parsed)
	if err != nil {
		h.logger.Errorw("WebAuthn FinishLogin failed", "error", err)
		writeError(w, http.StatusUnauthorized, "authentication failed")
		return
	}

	if err := h.checkOwnerMatches(credential.ID, body, session.Challenge); err != nil {
		h.logger.Errorw("User DID did not match the credential's owner", "error", err)
		writeError(w, http.StatusUnauthorized, "authentication failed")
		return
	}

	// The passkey proved the authenticator. Which account that authenticator
	// speaks for is a question am.toml answers, and it answers it now rather
	// than at enrolment — so striking an account out revokes its devices.
	admittedAs, err := h.creds.admittedAs(credential.ID)
	if err != nil {
		h.logger.Errorw("Failed to read the credential's admitting identity", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to read the credential")
		return
	}
	if h.identitiesGovern() && !h.stillAdmitted(admittedAs) {
		// Who this passkey speaks for, and what the deployment is checking
		// against, are both answers to a caller who has not been admitted.
		// The log keeps them; the response says only that the door is shut.
		h.logger.Infow("Passkey login refused", "admitted_as", admittedAs,
			"reason", "not listed in auth.root_identities")
		h.attest(PredicateRefused, admittedAs, map[string]any{
			"provider": "passkey",
			"reason":   "the identity this device speaks for is no longer listed",
		})
		writeError(w, http.StatusForbidden, "this credential may not log in here")
		return
	}

	if err := h.creds.updateSignCount(credential.ID, credential.Authenticator.SignCount); err != nil {
		h.logger.Errorw("Credential sign count not advanced; clone detection for this key is now blind", "error", err)
	}

	// A half-admission from laye is spent by the device that answers it.
	h.pendingLogins.close(heldPending(r))
	h.clearPendingCookie(w)

	token, err := h.sessions.create(admittedAs)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	h.setSessionCookie(w, token)

	h.attest(PredicateLoggedIn, admittedAs, map[string]any{
		"provider": "passkey",
		"device":   "asserted",
	})
	h.logger.Infow("WebAuthn authentication successful", "admitted_as", admittedAs)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	cookie, err := r.Cookie(sessionCookieName)
	if err == nil {
		// Read who before invalidating: afterwards the token names nobody,
		// which is the point of invalidating it.
		if who, ok := h.sessions.identityOf(cookie.Value); ok {
			h.attest(PredicateLoggedOut, who, map[string]any{"by": "logout"})
		}
		h.sessions.invalidate(cookie.Value)
	}

	h.clearSessionCookie(w)

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

// setSessionCookie writes the passkey session cookie. Secure flag is driven
// by Handler.secureCookies — set to true when the server is bound to a
// non-loopback address and thus expected to be served over TLS. Forcing
// Secure over plain http://localhost would silently drop the cookie in
// browsers, so dev over loopback keeps it off. Www-readiness P1.
func (h *Handler) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearSessionCookie writes an expiry cookie matching setSessionCookie's flags
// so browsers accept the deletion.
func (h *Handler) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}
