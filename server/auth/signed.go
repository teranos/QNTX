package auth

import (
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	errors "github.com/teranos/sacred-error"
)

// "A signature proves the key, and nothing reusable leaves the node."

// signedScheme is the Authorization scheme a signed request carries: a JWT its
// holder signed for this one request, with the key its did:key names.
const signedScheme = "Signed "

// signedOnly is the hash a grant is held under when nothing was handed out
// for it: no presented string hashes to it, so only a signature reaches it.
func signedOnly(did string) string {
	return "signed:" + did
}

// signedAssertion is the JWT a request carries under the Signed scheme.
func signedAssertion(r *http.Request) (string, bool) {
	signed, ok := strings.CutPrefix(r.Header.Get("Authorization"), signedScheme)
	signed = strings.TrimSpace(signed)
	return signed, ok && signed != ""
}

// signedGrant is the grant a signature stands on: signed with EdDSA by the key
// its issuer's did:key names, about itself, for this node, short-lived, and
// presented once.
func (h *Handler) signedGrant(signed string, now time.Time) (Grant, error) {
	var claims jwt.RegisteredClaims
	parsed, err := jwt.ParseWithClaims(signed, &claims, func(token *jwt.Token) (any, error) {
		issuer, err := token.Claims.GetIssuer()
		if err != nil {
			return nil, errors.Wrap(err, "the signature's issuer does not read")
		}
		return DecodeUserDID(issuer)
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithAudience(h.publicOrigin()),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil {
		return Grant{}, errors.Wrap(err, "the signature does not verify")
	}
	if !parsed.Valid {
		return Grant{}, errors.New("the signature does not verify")
	}
	if claims.Subject != claims.Issuer {
		return Grant{}, errors.Newf("the signature is issued by %q about %q, and a key signs as itself", claims.Issuer, claims.Subject)
	}
	if claims.ExpiresAt.Time.After(now.Add(assertionMaxLife)) {
		return Grant{}, errors.Newf("the signature lasts until %s, past the %s one may", claims.ExpiresAt.Time.UTC().Format(time.RFC3339), assertionMaxLife)
	}
	id, err := hex.DecodeString(claims.ID)
	if err != nil || len(id) != assertionIDBytes {
		return Grant{}, errors.Newf("the signature's jti %q is not %d random bytes in hex", claims.ID, assertionIDBytes)
	}
	if prior, fresh := h.assertions.spend(claims.ID, claims.ExpiresAt.Time, now); !fresh {
		return Grant{}, errors.Newf("the signature %s was already presented, and is kept until %v", claims.ID, prior)
	}
	held, live := h.askingToken(claims.Issuer)
	if !live {
		return Grant{}, errors.Newf("this node holds no live grant for %s", claims.Issuer)
	}
	return grantOfInfo(held), nil
}

// grantOfInfo is a listed record as the gate reads it.
func grantOfInfo(t TokenInfo) Grant {
	return Grant{
		ID:                  t.ID,
		Label:               t.Label,
		DID:                 t.DID,
		MintedBy:            t.MintedBy,
		MintedByUser:        t.MintedByUser,
		MintedByDisplayName: t.MintedByDisplayName,
		Level:               t.Level,
		Namespaces:          t.Namespaces,
		ReturnAddress:       t.ReturnAddress,
		ClientDID:           t.ClientDID,
		RequestID:           t.RequestID,
	}
}
