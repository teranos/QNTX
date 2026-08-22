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

// Walking back out of the door, and taking the device with you.

// Logging out ends a session. This ends the device: the credential is deleted,
// the keys it stood on come off the User, and the next arrival here is a
// stranger. Destructive, so the credential itself is what says which one.

// handleForgetBegin starts the ceremony that names the credential to drop. A
// session is the gate: forgetting is something the person at the device does.
// POST /auth/forget/begin
func (h *Handler) handleForgetBegin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if _, ok := h.signedInAs(r); !ok {
		writeError(w, http.StatusForbidden, "sign in before asking this device to forget you")
		return
	}

	creds, err := h.creds.getAll()
	if err != nil || len(creds) == 0 {
		writeError(w, http.StatusBadRequest, "this node holds no credentials to forget")
		return
	}

	options, session, err := h.webauthn.BeginLogin(&ownerUser{credentials: creds})
	if err != nil {
		h.logger.Errorw("WebAuthn BeginLogin failed for a forget", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to begin forgetting")
		return
	}

	h.ceremonies.Store(ownerUserID, session)
	writeJSON(w, http.StatusOK, options)
}

// forgetRequest carries the browser's own key alongside the assertion, because
// a device that has forgotten you should not still know this tab.
type forgetRequest struct {
	LayeDID string `json:"laye_did"`
}

// handleForget deletes the credential that just answered, and everything on the
// User that stood on it.
// POST /auth/forget
func (h *Handler) handleForget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	route, ok := h.signedInAs(r)
	if !ok {
		writeError(w, http.StatusForbidden, "sign in before asking this device to forget you")
		return
	}

	sessionVal, held := h.ceremonies.LoadAndDelete(ownerUserID)
	if !held {
		writeError(w, http.StatusBadRequest, "no forget ceremony in progress")
		return
	}
	// Anything else under that key is a wiring mistake. Refusing to forget is a
	// better answer to it than panicking inside a request.
	session, isSession := sessionVal.(*webauthn.SessionData)
	if !isSession {
		h.logger.Errorw("the ceremony store held something that is not a WebAuthn session",
			"route", route, "held", fmt.Sprintf("%T", sessionVal))
		writeError(w, http.StatusInternalServerError, "the forget ceremony is not readable")
		return
	}

	creds, err := h.creds.getAll()
	if err != nil || len(creds) == 0 {
		writeError(w, http.StatusBadRequest, "this node holds no credentials to forget")
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxCeremonyBodyBytes))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read the forget response")
		return
	}

	parsed, err := protocol.ParseCredentialRequestResponseBody(bytes.NewReader(body))
	if err != nil {
		h.logger.Errorw("the forget response did not parse", "error", err)
		writeError(w, http.StatusUnauthorized, "this device did not prove itself")
		return
	}

	credential, err := h.webauthn.ValidateLogin(&ownerUser{credentials: creds}, *session, parsed)
	if err != nil {
		h.logger.Errorw("the forget assertion did not validate", "error", err)
		writeError(w, http.StatusUnauthorized, "this device did not prove itself")
		return
	}

	// Whose device this was, read before it is deleted.
	ownerDID, err := h.creds.ownerOf(credential.ID)
	if err != nil {
		h.logger.Errorw("could not read the owner of a credential being forgotten", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to read the credential")
		return
	}

	if err := h.creds.forget(credential.ID); err != nil {
		h.logger.Errorw("could not delete a credential", "route", route, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to forget this device")
		return
	}


	var wanted forgetRequest
	if err := json.Unmarshal(body, &wanted); err != nil {
		h.logger.Warnw("a forget body carried no browser key", "route", route, "error", err)
	}
	h.dropKeys(route, ownerDID, wanted.LayeDID)

	// The session stood on the credential that is gone.
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		h.sessions.invalidate(cookie.Value)
	}
	h.clearSessionCookie(w)
	h.pendingLogins.close(heldPending(r))
	h.clearPendingCookie(w)

	h.logger.Infow("device forgotten", "route", route, "owner_did", ownerDID)
	h.attest(PredicateLoggedOut, route, map[string]any{"by": "forget"})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// signedInAs is the route this request's session was admitted as.
func (h *Handler) signedInAs(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return "", false
	}
	return h.sessions.identityOf(cookie.Value)
}

// dropKeys takes the keys a forgotten device stood on off the User. Recording
// never fails the thing it records, so a device that is gone stays gone even
// when the User cannot be written.
func (h *Handler) dropKeys(route string, dids ...string) {
	if h.users == nil {
		return
	}

	u, found, err := h.users.ByRoute(route)
	if err != nil || !found {
		h.logger.Warnw("could not read the User a forgotten device belonged to",
			"route", route, "found", found, "error", err)
		return
	}

	kept := make([]UserKey, 0, len(u.Keys))
	for _, k := range u.Keys {
		drop := false
		for _, did := range dids {
			if did != "" && k.DID == did {
				drop = true
			}
		}
		if !drop {
			kept = append(kept, k)
		}
	}
	if len(kept) == len(u.Keys) {
		return
	}

	u.Keys = kept
	if err := h.users.Put(u); err != nil {
		h.logger.Errorw("could not drop the keys of a forgotten device",
			"user", u.ID, "route", route, "error", err)
	}
}
