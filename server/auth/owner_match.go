package auth

import "github.com/teranos/errors"

// checkOwnerMatches confirms a login proves the DID its credential was
// registered with. The same biometric derives the same key, so a different
// DID is a different person or a different authenticator.
func (h *Handler) checkOwnerMatches(credID, body []byte, challenge string) error {
	stored, err := h.creds.ownerOf(credID)
	if err != nil {
		return err
	}

	proven, err := verifiedOwnerDID(body, challenge)
	if err != nil {
		return err
	}

	// Migration 054 removed the ownerless rows and forbids new ones, so an
	// empty owner here is a row that should not exist rather than a legacy
	// one to accommodate.
	if stored == "" {
		return errors.New("credential has no owner and cannot be trusted to speak for one")
	}

	if proven != stored {
		return errors.Newf("credential is owned by %s, login proved %q", stored, proven)
	}
	return nil
}
