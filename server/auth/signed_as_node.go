package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/teranos/QNTX/internal/access"
	errors "github.com/teranos/sacred-error"
)

// SignedAsNode is the Authorization a request to origin goes with: a JWT the
// node's own key signs as its did:key, for that origin, once, for a minute.
// laye takes it as the node (laye-front who_signed).
func (h *Handler) SignedAsNode(origin string) (string, error) {
	if len(h.nodeKey) != ed25519.PrivateKeySize {
		return "", errors.New("this node holds no key of its own to sign with")
	}
	did := access.EncodeDIDKey(h.nodeKey.Public().(ed25519.PublicKey))
	jti := make([]byte, 16)
	if _, err := rand.Read(jti); err != nil {
		return "", errors.Wrap(err, "no id could be drawn for the signature")
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.RegisteredClaims{
		Issuer: did, Subject: did, Audience: jwt.ClaimStrings{origin},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)), ID: hex.EncodeToString(jti),
	}).SignedString(h.nodeKey)
	if err != nil {
		return "", errors.Wrapf(err, "the request to %s was not signed", origin)
	}
	return signedScheme + signed, nil
}
