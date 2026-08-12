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

	// Credentials enrolled before PRF have no owner and must keep working —
	// shipping otherwise would lock out every passkey already registered.
	if stored == "" {
		return nil
	}

	if proven != stored {
		return errors.Newf("credential is owned by %s, login proved %q", stored, proven)
	}
	return nil
}
