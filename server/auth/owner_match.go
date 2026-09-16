package auth

import (
	"net/http"
	"slices"

	"github.com/teranos/errors"
)

// A passkey stands on several devices, and each derives its own key from it.
// A login proving a key the passkey has not stood on before is not another
// person: it is the same passkey on one more device, and the ceremony it is
// answering is provider-proven, so the device is recorded rather than refused.
//
// "Clearly I want the honest model."

// ownerStands reads the key a login proved and whether the credential already
// stands on it. An error is a login that proved nothing, or a credential
// nobody enrolled; a key that does not stand yet is an answer, not an error.
func (h *Handler) ownerStands(credID, body []byte, challenge string) (proven string, stands bool, err error) {
	owners, err := h.creds.ownersOf(credID)
	if err != nil {
		return "", false, err
	}
	// Migration 054 removed the ownerless rows and forbids new ones, so a
	// credential standing nowhere is one that should not exist rather than a
	// legacy one to accommodate.
	if len(owners) == 0 {
		return "", false, errors.New("credential stands on no device and cannot be trusted to speak for one")
	}

	proven, err = verifiedOwnerDID(body, challenge)
	if err != nil {
		return "", false, err
	}
	// An owned credential presented without a proof is a downgrade attempt:
	// drop the key and inherit the session it was meant to gate.
	if proven == "" {
		return "", false, errors.New("the login proved no owner key for a credential that stands on one")
	}
	return proven, slices.Contains(owners, proven), nil
}

// standOn records that a passkey now stands on this device: one more owner of
// the credential, and one more device key the User holds (ADR-031).
func (h *Handler) standOn(credID []byte, ownerDID, admittedAs string) error {
	if err := h.creds.addOwner(credID, ownerDID, admittedAs); err != nil {
		return err
	}
	h.joinDeviceKey(admittedAs, ownerDID)
	h.logger.Infow("a passkey stands on a new device", "admitted_as", admittedAs, "owner_did", ownerDID)
	return nil
}

// refusePasskey turns a passkey ceremony away. The log keeps the reason, the
// attestation records it (ADR-030), and the caller is told only that the door
// is shut. Six gates said this in six copies.
func (h *Handler) refusePasskey(w http.ResponseWriter, ceremony, admittedAs, why, recorded, told string) {
	h.logger.Infow("Passkey "+ceremony+" refused", "admitted_as", admittedAs, "reason", why)
	h.attest(PredicateRefused, admittedAs, map[string]any{"provider": "passkey", "reason": recorded})
	h.writeError(w, http.StatusForbidden, told)
}
