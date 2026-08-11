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
