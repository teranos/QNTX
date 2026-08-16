package auth

import (
	"net/http"

	"github.com/teranos/errors"
)

// mayRegister decides whether this request may enrol a passkey. A deployment
// with no credentials is open, because first enrolment has nobody to ask.
func (h *Handler) mayRegister(r *http.Request) error {
	registered, err := h.creds.exists()
	if err != nil {
		return errors.Wrap(err, "failed to check whether a credential is registered")
	}
	if !registered {
		return nil
	}

	// After that a session is required: adding a device is something the owner
	// does, and without this any caller could enrol themselves alongside.
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || !h.sessions.validate(cookie.Value) {
		return errors.New("a passkey is already registered; sign in before adding another device")
	}
	return nil
}

// enrollingIdentity is who this enrolment speaks for: a signed-in session
// adding a second device, or a half-admission whose first device this is.
func (h *Handler) enrollingIdentity(r *http.Request) string {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		if identity, ok := h.sessions.identityOf(cookie.Value); ok {
			return identity
		}
	}
	// Why laye stops short of a session: without this the first login for an
	// account could never enrol, because enrolling required the session that
	// enrolling was supposed to produce.
	if identity, ok := h.pendingLogins.peek(heldPending(r)); ok {
		return identity
	}
	return ""
}

// quoteIdentity renders an identity for a message, so "nobody" and a name are
// visibly different answers rather than one of them being a blank.
func quoteIdentity(identity string) string {
	if identity == "" {
		return "no identity"
	}
	return identity
}
