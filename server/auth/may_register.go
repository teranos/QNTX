package auth

import "github.com/teranos/errors"

// mayRegister decides whether this request may enrol a passkey. A deployment
// is never open: enrolling needs an identity that has been admitted, and a
// deployment naming nobody has admitted nobody.
func (h *Handler) mayRegister(p Presented) error {
	who, enrolling := p.Enrolling()
	if !enrolling {
		return errors.New("enrolling a passkey needs an identity that has been admitted")
	}

	// Asked here and again at save. The list moves under a ceremony, and a
	// device enrolled between the two would outlive the account it speaks for.
	if !h.stillAdmitted(who) {
		return errors.Newf(
			"%s is not listed in auth.root_identities, so no device may be enrolled for it",
			quoteIdentity(who))
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
