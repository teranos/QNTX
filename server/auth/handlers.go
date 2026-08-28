package auth

import (
	"bytes"
	"encoding/json"
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
		h.writeError(w, http.StatusInternalServerError, "the credential store did not answer")
		return
	}

	// Empty means a passkey exists but no identity was established — the
	// pre-#577 state. Reporting it lets the UI say so instead of implying one.
	ownerDID, err := h.creds.owner()
	if err != nil {
		h.logger.Errorw("Failed to read the registered owner", "error", err)
		h.writeError(w, http.StatusInternalServerError, "the credential store did not answer")
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
	p := h.presented(r)
	identity, _ := p.Admitted()

	// A token speaks for whoever minted it (ADR-025), and the door is drawn on
	// this field alone. Reporting nobody sent a caller Middleware already
	// grants SUPER to a passkey prompt no token can answer.
	if identity == "" && p.Bearer != nil && h.stillAdmitted(p.Bearer.MintedBy) {
		identity = p.Bearer.MintedBy
	}

	h.writeJSON(w, http.StatusOK, map[string]any{
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

	if err := h.mayRegister(h.presented(r)); err != nil {
		h.logger.Warnw("Passkey enrolment refused", "error", err)
		h.writeError(w, http.StatusUnauthorized, err.Error())
		return
	}

	user := &ownerUser{}
	options, session, err := h.webauthn.BeginRegistration(user,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementDiscouraged),
	)
	if err != nil {
		h.logger.Errorw("WebAuthn BeginRegistration failed", "error", err)
		h.writeError(w, http.StatusInternalServerError, "the ceremony was not started")
		return
	}

	h.ceremonies.Store(ownerUserID, session)
	h.writeJSON(w, http.StatusOK, options)
}

func (h *Handler) handleRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	p := h.presented(r)

	sessionVal, ok := h.ceremonies.LoadAndDelete(ownerUserID)
	if !ok {
		h.writeError(w, http.StatusBadRequest, "no registration ceremony")
		return
	}
	session := sessionVal.(*webauthn.SessionData)

	// The body carries both the WebAuthn response and the user DID proof, and
	// the library consumes the request, so read it once and parse it twice.
	body, err := io.ReadAll(io.LimitReader(r.Body, maxCeremonyBodyBytes))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "the body was not read")
		return
	}

	parsed, err := protocol.ParseCredentialCreationResponseBody(bytes.NewReader(body))
	if err != nil {
		h.logger.Errorw("WebAuthn registration response did not parse", "error", err)
		h.writeError(w, http.StatusBadRequest, "the attestation did not parse")
		return
	}

	user := &ownerUser{}
	credential, err := h.webauthn.CreateCredential(user, *session, parsed)
	if err != nil {
		h.logger.Errorw("WebAuthn FinishRegistration failed", "error", err)
		h.writeError(w, http.StatusBadRequest, "the attestation did not validate")
		return
	}

	if len(credential.ID) > 1024 {
		h.writeError(w, http.StatusBadRequest, "the credential id is longer than 1024 bytes")
		return
	}
	if len(credential.PublicKey) > 4096 {
		h.writeError(w, http.StatusBadRequest, "the public key is longer than 4096 bytes")
		return
	}

	// #577: the browser derives this from the WebAuthn PRF output and signs
	// the ceremony challenge with it.
	ownerDID, err := verifiedOwnerDID(body, session.Challenge)
	if err != nil {
		h.logger.Errorw("User DID proof rejected", "error", err)
		h.writeError(w, http.StatusBadRequest, "user identity proof rejected")
		return
	}
	// An authenticator that will not say which key it is has no provenance.
	if ownerDID == "" {
		h.logger.Warnw("Passkey enrolment refused: the browser proved no owner key",
			"reason", "WebAuthn PRF produced nothing")
		h.writeError(w, http.StatusBadRequest, "no owner key was proven")
		return
	}

	// The session that authorized this enrolment says which account the new
	// passkey speaks for. Unconditional: a deployment listing nobody has
	// nobody to enrol on behalf of.
	admittedAs, enrolling := p.Enrolling()
	if !enrolling {
		h.logger.Warnw("Passkey enrolment refused: the session names no identity",
			"root_identities", len(h.identities.roots()))
		h.writeError(w, http.StatusForbidden, "no admission")
		return
	}

	// Asked again here, not carried from the gate. A ceremony takes as long as
	// a person takes, and am.toml can be rewritten inside that window — login
	// re-asks for the same reason (ADR-030).
	if !h.stillAdmitted(admittedAs) {
		h.logger.Infow("Passkey enrolment refused", "admitted_as", admittedAs,
			"reason", "no longer listed in auth.root_identities")
		h.attest(PredicateRefused, admittedAs, map[string]any{
			"provider": "passkey",
			"reason":   "the identity this device would speak for is no longer listed",
		})
		h.writeError(w, http.StatusForbidden, admittedAs+" is not listed")
		return
	}

	if err := h.creds.save(*credential, ownerDID, admittedAs); err != nil {
		h.logger.Errorw("Failed to save credential", "error", err)
		h.writeError(w, http.StatusInternalServerError, "the credential was not written")
		return
	}

	// One more place this User can be reached from, and the only kind of key a
	// finger produces (ADR-031).
	h.joinDeviceKey(admittedAs, ownerDID)

	// The half-admission is spent here, so one laye signature buys one device.
	h.spend(p, w)

	// Resolved once, here, so no request after this has to scan for it.
	token, err := h.sessions.create(admittedAs, h.userFor(admittedAs))
	if err != nil {
		h.logger.Errorw("a passkey enrolled but no session could be made for it",
			"admitted_as", admittedAs, "owner", ownerDID, "error", err)
		h.writeError(w, http.StatusInternalServerError, "the session was not created")
		return
	}
	h.setSessionCookie(w, token)

	h.attest(PredicateLoggedIn, admittedAs, map[string]any{
		"provider": "passkey",
		"device":   "enrolled",
		"owner":    ownerDID,
	})
	h.logger.Infow("WebAuthn credential registered and session created", "admitted_as", admittedAs)
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleLoginBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// A passkey is the second half of an admission, never the whole of one.
	if _, ok := h.presented(r).HalfAdmitted(); !ok {
		h.writeError(w, http.StatusForbidden, "no half-admission")
		return
	}

	creds, err := h.creds.getAll()
	if err != nil {
		h.logger.Errorw("could not read the credentials to begin a login", "error", err)
		h.writeError(w, http.StatusInternalServerError, "the credential store did not answer")
		return
	}
	if len(creds) == 0 {
		h.writeError(w, http.StatusBadRequest, "no credentials")
		return
	}

	user := &ownerUser{credentials: creds}
	options, session, err := h.webauthn.BeginLogin(user)
	if err != nil {
		h.logger.Errorw("WebAuthn BeginLogin failed", "error", err)
		h.writeError(w, http.StatusInternalServerError, "the ceremony was not started")
		return
	}

	h.ceremonies.Store(ownerUserID, session)
	h.writeJSON(w, http.StatusOK, options)
}

func (h *Handler) handleLoginFinish(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	p := h.presented(r)
	if _, ok := p.HalfAdmitted(); !ok {
		h.writeError(w, http.StatusForbidden, "no half-admission")
		return
	}

	sessionVal, ok := h.ceremonies.LoadAndDelete(ownerUserID)
	if !ok {
		h.writeError(w, http.StatusBadRequest, "no login ceremony")
		return
	}
	session := sessionVal.(*webauthn.SessionData)

	creds, err := h.creds.getAll()
	if err != nil {
		h.logger.Errorw("could not read the credentials to finish a login", "error", err)
		h.writeError(w, http.StatusInternalServerError, "the credential store did not answer")
		return
	}
	if len(creds) == 0 {
		h.writeError(w, http.StatusBadRequest, "no credentials")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxCeremonyBodyBytes))
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "the body was not read")
		return
	}

	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(body))
	if err != nil {
		h.logger.Errorw("WebAuthn login response did not parse", "error", err)
		h.writeError(w, http.StatusUnauthorized, "the assertion did not parse")
		return
	}

	user := &ownerUser{credentials: creds}
	credential, err := h.webauthn.ValidateLogin(user, *session, parsed)
	if err != nil {
		h.logger.Errorw("WebAuthn FinishLogin failed", "error", err)
		h.writeError(w, http.StatusUnauthorized, "the assertion did not validate")
		return
	}

	if err := h.checkOwnerMatches(credential.ID, body, session.Challenge); err != nil {
		h.logger.Errorw("User DID did not match the credential's owner", "error", err)
		h.writeError(w, http.StatusUnauthorized, "the owner did not match")
		return
	}

	// The passkey proved the authenticator. Which account that authenticator
	// speaks for is a question am.toml answers, and it answers it now rather
	// than at enrolment — so striking an account out revokes its devices.
	admittedAs, err := h.creds.admittedAs(credential.ID)
	if err != nil {
		h.logger.Errorw("Failed to read the credential's admitting identity", "error", err)
		h.writeError(w, http.StatusInternalServerError, "the credential store did not answer")
		return
	}

	if !h.stillAdmitted(admittedAs) {
		// Who this passkey speaks for, and what the deployment is checking
		// against, are both answers to a caller who has not been admitted.
		// The log keeps them; the response says only that the door is shut.
		h.logger.Infow("Passkey login refused", "admitted_as", admittedAs,
			"reason", "not listed in auth.root_identities")
		h.attest(PredicateRefused, admittedAs, map[string]any{
			"provider": "passkey",
			"reason":   "the identity this device speaks for is no longer listed",
		})
		h.writeError(w, http.StatusForbidden, admittedAs+" is not listed")
		return
	}

	if err := h.creds.updateSignCount(credential.ID, credential.Authenticator.SignCount); err != nil {
		h.logger.Errorw("Credential sign count not advanced; clone detection for this key is now blind", "error", err)
	}

	// A half-admission from laye is spent by the device that answers it.
	h.spend(p, w)

	// Resolved once, here, so no request after this has to scan for it.
	token, err := h.sessions.create(admittedAs, h.userFor(admittedAs))
	if err != nil {
		h.logger.Errorw("a passkey answered but no session could be made for it",
			"admitted_as", admittedAs, "error", err)
		h.writeError(w, http.StatusInternalServerError, "the session was not created")
		return
	}
	h.setSessionCookie(w, token)

	h.attest(PredicateLoggedIn, admittedAs, map[string]any{
		"provider": "passkey",
		"device":   "asserted",
	})
	h.logger.Infow("WebAuthn authentication successful", "admitted_as", admittedAs)
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	p := h.presented(r)
	// Read who before invalidating: afterwards the token names nobody, which
	// is the point of invalidating it.
	if who, ok := p.Admitted(); ok {
		h.attest(PredicateLoggedOut, who, map[string]any{"by": "logout"})
	}
	h.sessions.invalidate(p.sessionToken)

	h.clearSessionCookie(w)

	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// --- Response helpers ---

// Methods, not package functions, so a failed write has a logger in scope:
// the status line cannot be resent, but the failure can exist somewhere.

func (h *Handler) writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Errorw("Auth response failed to send after its status was written",
			"status", status, "error", err)
	}
}

func (h *Handler) writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": message}); err != nil {
		h.logger.Errorw("Auth error response failed to send after its status was written",
			"status", status, "intended_error", message, "error", err)
	}
}

// setSessionCookie writes the passkey session cookie. Handler.secureCookies is
// true when auth.rp_origins says a browser reaches this deployment over https;
// forcing Secure over plain http://localhost would drop the cookie silently.
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
