package auth

import "github.com/teranos/errors"

// mayRegister decides whether this request may enrol a passkey. A deployment
// naming nobody and holding no credentials is open, because first enrolment
// has nobody to ask.
func (h *Handler) mayRegister(p Presented) error {
	// A deployment that names who may log in is never open, however empty it
	// is. Without this the openness rests on handleRegisterFinish refusing an
	// ownerless credential at save — correct, but stated nowhere.
	if h.identitiesGovern() {
		if _, enrolling := p.Enrolling(); !enrolling {
			return errors.New("enrolling a passkey needs an identity that has been admitted")
		}
		return nil
	}

	registered, err := h.creds.exists()
	if err != nil {
		return errors.Wrap(err, "failed to check whether a credential is registered")
	}
	if !registered {
		return nil
	}

	// After that a session is required: adding a device is something the owner
	// does, and without this any caller could enrol themselves alongside.

	// The session itself is the check, not what it is called — an ungoverned
	// deployment issues sessions that name nobody.
	if _, signedIn := p.Admitted(); !signedIn {
		return errors.New("a passkey is already registered; sign in before adding another device")
	}
	return nil
}

// quoteIdentity renders an identity for a message, so "nobody" and a name are
// visibly different answers rather than one of them being a blank.
func quoteIdentity(identity string) string {
	if identity == "" {
		return "no identity"
	}
	return identity
}
