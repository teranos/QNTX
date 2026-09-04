package auth

import "github.com/teranos/errors"

// mayRegister decides whether this request may enrol a passkey. A deployment
// is never open: enrolling needs an identity that has been admitted, and a
// deployment naming nobody has admitted nobody.
func (h *Handler) mayRegister(p Presented) error {
	who, enrolling := p.Enrolling()
	if !enrolling {
		return errRefused
	}

	// Asked here and again at save. The list moves under a ceremony, and a
	// device enrolled between the two would outlive the account it speaks for.
	if !h.stillAdmitted(who) {
		return errRefused
	}
	return nil
}

// errRefused is what a refused caller is told. The node writes down who was
// refused and why as an attestation (ADR-030); a caller who did not get in
// learns the outcome and nothing that helps them get in next time.
var errRefused = errors.New("refused")

// quoteIdentity renders an identity for a log line, so a blank and a name are
// visibly different rather than one of them being nothing.
func quoteIdentity(identity string) string {
	if identity == "" {
		return "no identity"
	}
	return identity
}
