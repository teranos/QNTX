package auth

import "github.com/teranos/errors"

// mayRegister decides whether this request may enrol a passkey. A deployment
// is never open: enrolling needs an identity that has been admitted, and a
// deployment naming nobody has admitted nobody.
func (h *Handler) mayRegister(p Presented) error {
	who, enrolling := p.Enrolling()
	if !enrolling {
		return errors.New("no admission")
	}

	// Asked here and again at save. The list moves under a ceremony, and a
	// device enrolled between the two would outlive the account it speaks for.
	if !h.stillAdmitted(who) {
		return errors.New(notListed(who))
	}
	return nil
}

// quoteIdentity renders an identity for a log line, so a blank and a name are
// visibly different rather than one of them being nothing.
func quoteIdentity(identity string) string {
	if identity == "" {
		return "no identity"
	}
	return identity
}

// notListed is the fact, for a session that named an identity and for one that
// named none. Pasting the two together reads as neither.
func notListed(identity string) string {
	if identity == "" {
		return "no identity"
	}
	return identity + " is not listed"
}
