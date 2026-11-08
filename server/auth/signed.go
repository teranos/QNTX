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

// "「[Sonnet 5.5] A signature proves the key, and nothing reusable leaves the node.」"

// 「[Opus 5.5] signedScheme is the Authorization scheme a signed request carries: a JWT its」
// 「[Opus 5.5] holder signed for this one request, with the key its did:key names.」
const signedScheme = "Signed "

// 「[Opus 5.5] signedOnly is the hash a grant is held under when nothing was handed out」
// 「[Opus 5.5] for it: no presented string hashes to it, so only a signature reaches it.」
func signedOnly(did string) string {
	return "signed:" + did
}

// 「[Opus 5.5] signedAssertion is the JWT a request carries under the Signed scheme.」
func signedAssertion(r *http.Request) (string, bool) {
	signed, ok := strings.CutPrefix(r.Header.Get("Authorization"), signedScheme)
	signed = strings.TrimSpace(signed)
	return signed, ok && signed != ""
}

// 「[Opus 5.5] signedGrant is the grant a signature stands on. A key signing as itself」
// 「[Opus 5.5] stands on the grant this node holds for its DID; the primary signing for a」
// 「[Opus 5.5] person stands on what it signed, admitted by this node's own lines. Either」
// 「[Opus 5.5] way: EdDSA by the key its issuer's did:key names, short-lived, presented once.」
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
	// 「[Opus 5.5] Whose signature this is is asked before its id is kept: a key nobody」
	// 「[Opus 5.5] granted anything fills nothing here.」
	var held TokenInfo
	if carried {
		if !h.signsForItsPeople(claims.Issuer) {
			return Grant{}, errors.Newf("%s signed for %q, and it is not this node's primary", claims.Issuer, claims.Subject)
		}
	} else {
		var live bool
		if held, live = h.askingToken(claims.Issuer); !live {
			return Grant{}, errors.Newf("this node holds no live grant for %s", claims.Issuer)
		}
	}
	if prior, fresh := h.assertions.spend(claims.ID, claims.ExpiresAt.Time, now); !fresh {
		return Grant{}, errors.Newf("the signature %s was already presented, and is kept until %v", claims.ID, prior)
	}
	if carried {
		return h.carriedGrant(claims)
	}
	return grantOfInfo(held), nil
}

// 「[Opus 5.5] carriedGrant is the person the primary signed for, as this node's own User」
// 「[Opus 5.5] for their route: the primary's User is the primary's record.」
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

// 「[Opus 5.5] signsForItsPeople is whether did is this node's primary (nodes.primary), as」
// 「[Opus 5.5] the primary says of itself where this node already asks it about tokens. A」
// 「[Opus 5.5] signer other than the one kept is asked about again, once the kept answer is」
// 「[Opus 5.5] older than primaryDIDFresh: a primary whose key changed is heard.」
func (h *Handler) signsForItsPeople(did string) bool {
	for _, issuer := range h.issuers {
		said, err := h.primaryDID(issuer, false)
		if err == nil && said != did {
			said, err = h.primaryDID(issuer, true)
		}
		if err != nil {
			h.logger.Errorw("The primary did not say its DID, so what it signed is not admitted", "issuer", issuer, "error", err)
			continue
		}
		if said == did {
			return true
		}
	}
	return false
}

// 「[Opus 5.5] primaryDIDFresh is how long the DID a primary said is kept before a signer」
// 「[Opus 5.5] other than it makes the node ask again.」
const primaryDIDFresh = time.Minute

// 「[Opus 5.5] saidDID is what a primary said it signs as, and when.」
type saidDID struct {
	did string
	at  time.Time
}

// 「[Opus 5.5] primaryDID is the did:key an issuer says it signs as: kept, or asked when」
// 「[Opus 5.5] again is set and what is kept is older than primaryDIDFresh. A redirect is」
// 「[Opus 5.5] not followed: the answer comes from the origin nodes.primary names.」
func (h *Handler) primaryDID(issuer string, again bool) (string, error) {
	if held, found := h.issuerDIDs.Load(issuer); found {
		if kept, isKept := held.(saidDID); isKept && (!again || time.Since(kept.at) < primaryDIDFresh) {
			return kept.did, nil
		}
	}
	client := http.Client{Timeout: issuerTimeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
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
	h.issuerDIDs.Store(issuer, saidDID{did: said.DID, at: time.Now()})
	return said.DID, nil
}

// 「[Opus 5.5] grantOfInfo is a listed record as the gate reads it.」
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
