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
	if _, ok := h.presented(r).Admitted(); !ok {
		h.writeError(w, http.StatusForbidden, "no session")
		return
	}

	creds, err := h.creds.getAll()
	if err != nil {
		h.logger.Errorw("could not read the credentials for a forget", "error", err)
		h.writeError(w, http.StatusInternalServerError, "the credential store did not answer: "+err.Error())
		return
	}
	if len(creds) == 0 {
		h.writeError(w, http.StatusBadRequest, "no credentials")
		return
	}

	options, session, err := h.webauthn.BeginLogin(&ownerUser{credentials: creds})
	if err != nil {
		h.logger.Errorw("WebAuthn BeginLogin failed for a forget", "error", err)
		h.writeError(w, http.StatusInternalServerError, "the ceremony was not started: "+err.Error())
		return
	}

	h.ceremonies.Store(ownerUserID, session)
	h.writeJSON(w, http.StatusOK, options)
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

	p := h.presented(r)
	route, ok := p.Admitted()
	if !ok {
		h.writeError(w, http.StatusForbidden, "no session")
		return
	}

	sessionVal, held := h.ceremonies.LoadAndDelete(ownerUserID)
	if !held {
		h.writeError(w, http.StatusBadRequest, "no forget ceremony")
		return
	}
	// Anything else under that key is a wiring mistake. Refusing to forget is a
	// better answer to it than panicking inside a request.
	session, isSession := sessionVal.(*webauthn.SessionData)
	if !isSession {
		h.logger.Errorw("the ceremony store held something that is not a WebAuthn session",
			"route", route, "held", fmt.Sprintf("%T", sessionVal))
		h.writeError(w, http.StatusInternalServerError, "the ceremony store held the wrong type")
		return
	}

	creds, err := h.creds.getAll()
	if err != nil {
		h.logger.Errorw("could not read the credentials for a forget", "route", route, "error", err)
		h.writeError(w, http.StatusInternalServerError, "the credential store did not answer: "+err.Error())
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
		h.logger.Errorw("the forget response did not parse", "error", err)
		h.writeError(w, http.StatusUnauthorized, "the assertion did not parse")
		return
	}

	credential, err := h.webauthn.ValidateLogin(&ownerUser{credentials: creds}, *session, parsed)
	if err != nil {
		h.logger.Errorw("the forget assertion did not validate", "error", err)
		h.writeError(w, http.StatusUnauthorized, "the assertion did not validate")
		return
	}

	// Whose device this was, read before it is deleted.
	ownerDID, err := h.creds.ownerOf(credential.ID)
	if err != nil {
		h.logger.Errorw("could not read the owner of a credential being forgotten", "error", err)
		h.writeError(w, http.StatusInternalServerError, "the credential store did not answer: "+err.Error())
		return
	}

	if err := h.creds.forget(credential.ID); err != nil {
		h.logger.Errorw("could not delete a credential", "route", route, "error", err)
		h.writeError(w, http.StatusInternalServerError, "the credential was not deleted: "+err.Error())
		return
	}

	var wanted forgetRequest
	if err := json.Unmarshal(body, &wanted); err != nil {
		h.logger.Warnw("a forget body carried no browser key", "route", route, "error", err)
	}
	h.dropKeys(route, ownerDID, wanted.LayeDID)

	// The session stood on the credential that is gone.
	h.sessions.invalidate(p.sessionToken)
	h.clearSessionCookie(w)
	h.spend(p, w)

	h.logger.Infow("device forgotten", "route", route, "owner_did", ownerDID)
	h.attest(PredicateLoggedOut, route, map[string]any{"by": "forget"})
	h.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
