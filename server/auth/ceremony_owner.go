package auth

import (
	"encoding/base64"
	"encoding/json"

	"github.com/teranos/errors"
)

// ceremonyProof is what the browser adds alongside the WebAuthn response.
// The PRF seed stays in the client; only the DID and a signature cross.
type ceremonyProof struct {
	UserDID   string `json:"user_did"`
	Signature string `json:"user_did_signature"`
}

// verifiedOwnerDID returns the user DID a ceremony body proves, empty when the
// client offered none. The caller refuses that: a credential with no owner
// answers to whoever holds the authenticator.
func verifiedOwnerDID(body []byte, challenge string) (string, error) {
	var proof ceremonyProof
	if err := json.Unmarshal(body, &proof); err != nil {
		return "", errors.Wrap(err, "failed to read the user DID proof from the ceremony response")
	}
	if proof.UserDID == "" {
		return "", nil
	}

	// A DID that is present but unproven is refused rather than dropped:
	// falling back to ownerless would let anyone claim any identity and still
	// be handed a session.
	signature, err := base64.RawURLEncoding.DecodeString(proof.Signature)
	if err != nil {
		return "", errors.Wrapf(err, "user DID %s carries an unreadable signature", proof.UserDID)
	}
	if err := VerifyUserDID(proof.UserDID, []byte(challenge), signature); err != nil {
		return "", err
	}
	return proof.UserDID, nil
}
