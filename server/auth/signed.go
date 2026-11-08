package auth

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"slices"
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

// signedGrant is the grant a signature stands on. A key signing as itself
// stands on the grant this node holds for its DID; the primary signing for a
// person stands on what it signed, admitted by this node's own lines. Either
// way: EdDSA by the key its issuer's did:key names, short-lived, presented once.
func (h *Handler) signedGrant(signed string, now time.Time) (Grant, error) {
	var claims carriedClaims
	parsed, err := jwt.ParseWithClaims(signed, &claims, func(token *jwt.Token) (any, error) {
		issuer, err := token.Claims.GetIssuer()
		if err != nil {
			return nil, errors.Wrap(err, "the signature's issuer does not read")
		}
		return DecodeUserDID(issuer)
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil {
		return Grant{}, errors.Wrap(err, "the signature does not verify")
	}
	if !parsed.Valid {
		return Grant{}, errors.New("the signature does not verify")
	}
	if claims.ExpiresAt.Time.After(now.Add(assertionMaxLife)) {
		return Grant{}, errors.Newf("the signature lasts until %s, past the %s one may", claims.ExpiresAt.Time.UTC().Format(time.RFC3339), assertionMaxLife)
	}
	id, err := hex.DecodeString(claims.ID)
	if err != nil || len(id) != assertionIDBytes {
		return Grant{}, errors.Newf("the signature's jti %q is not %d random bytes in hex", claims.ID, assertionIDBytes)
	}
	carried := claims.Subject != claims.Issuer
	audience := h.publicOrigin()
	if carried {
		if audience, err = h.ownDID(); err != nil {
			return Grant{}, err
		}
	}
	if !slices.Contains(claims.Audience, audience) {
		return Grant{}, errors.Newf("the signature is for %v, and this node is %s", claims.Audience, audience)
	}
	if carried && !h.signsForItsPeople(claims.Issuer) {
		return Grant{}, errors.Newf("%s signed for %q, and it is not this node's primary", claims.Issuer, claims.Subject)
	}
	if prior, fresh := h.assertions.spend(claims.ID, claims.ExpiresAt.Time, now); !fresh {
		return Grant{}, errors.Newf("the signature %s was already presented, and is kept until %v", claims.ID, prior)
	}
	if carried {
		return h.carriedGrant(claims)
	}
	held, live := h.askingToken(claims.Issuer)
	if !live {
		return Grant{}, errors.Newf("this node holds no live grant for %s", claims.Issuer)
	}
	return grantOfInfo(held), nil
}

// carriedGrant is the person the primary signed for, as this node's own User
// for their route: the primary's User is the primary's record.
func (h *Handler) carriedGrant(claims carriedClaims) (Grant, error) {
	here, found, err := h.users.ByRoute(claims.Subject)
	if err != nil {
		return Grant{}, errors.Wrapf(err, "the User store did not answer for %s", claims.Subject)
	}
	if !found {
		here = User{}
	}
	return Grant{
		Label:               claims.Label,
		DID:                 claims.Issuer,
		MintedBy:            claims.Subject,
		MintedByUser:        here.ID,
		MintedByDisplayName: claims.Name,
		Level:               Level(claims.Level),
		Namespaces:          claims.Namespaces,
		ClientDID:           claims.Client,
	}, nil
}

// signsForItsPeople is whether did is this node's primary (nodes.primary), as
// the primary says of itself where this node already asks it about tokens.
func (h *Handler) signsForItsPeople(did string) bool {
	for _, issuer := range h.issuers {
		if said, err := h.primaryDID(issuer); err == nil && said == did {
			return true
		} else if err != nil {
			h.logger.Errorw("The primary did not say its DID, so what it signed is not admitted", "issuer", issuer, "error", err)
		}
	}
	return false
}

// primaryDID is the did:key an issuer says it signs as, asked once and kept.
func (h *Handler) primaryDID(issuer string) (string, error) {
	if held, found := h.issuerDIDs.Load(issuer); found {
		if did, isDID := held.(string); isDID {
			return did, nil
		}
	}
	client := http.Client{Timeout: issuerTimeout}
	resp, err := client.Get(issuer + authorizationServerPath)
	if err != nil {
		return "", errors.Wrapf(err, "%s did not answer", issuer+authorizationServerPath)
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, issuerAnswerBytes))
	if closeErr := resp.Body.Close(); closeErr != nil {
		return "", errors.Wrap(closeErr, "the issuer's metadata did not close")
	}
	if readErr != nil {
		return "", errors.Wrap(readErr, "the issuer's metadata did not read")
	}
	if resp.StatusCode != http.StatusOK {
		return "", errors.Newf("%s answered %d: %s", issuer+authorizationServerPath, resp.StatusCode, body)
	}
	var said struct {
		DID string `json:"did"`
	}
	if err := json.Unmarshal(body, &said); err != nil {
		return "", errors.Wrapf(err, "the issuer's metadata is not JSON: %s", body)
	}
	if _, err := DecodeUserDID(said.DID); err != nil {
		return "", errors.Wrapf(err, "%s names no did:key it signs as", issuer)
	}
	h.issuerDIDs.Store(issuer, said.DID)
	return said.DID, nil
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
