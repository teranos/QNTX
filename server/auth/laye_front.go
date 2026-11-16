package auth

import (
	"strings"

	errors "github.com/teranos/sacred-error"
)

// "LAYE IN FRONT OF QNTX"
// "AND LAYE OWNS IDENTITY"
// "BUT LAYE DOES NOT REALLY OWN USER, THE QNTX WAY"

// SetLaye names the laye in front of this node by its did:key, or none. laye
// checks a session's own signature and hands the node its own, naming the
// session; the node admits it on the grant it holds for that session, and
// nothing laye says of the session's level or namespaces counts.
func (h *Handler) SetLaye(did string) error {
	did = strings.TrimSpace(did)
	if did != "" {
		if _, err := DecodeUserDID(did); err != nil {
			return errors.Wrapf(err, "laye.did %q is not a did:key", did)
		}
	}
	h.laye = did
	return nil
}

// carriedByLaye is whether a signature is laye's, carrying another key.
func (h *Handler) carriedByLaye(issuer, subject string) bool {
	return h.laye != "" && issuer == h.laye && subject != issuer
}
