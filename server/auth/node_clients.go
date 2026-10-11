package auth

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ory/fosite"
	errors "github.com/teranos/sacred-error"
)

// A node is a client of another node, registered by its own did:key. It
// proves itself by signing with that key (RFC 7523 §2.2), so no secret exists.

// "every QNTX node is an OAuth server and an OAuth client. Nodes authenticate
// to each other only through OAuth, over the public internet, with no network trusted"

// clientAssertionType is RFC 7523 §2.2's client_assertion_type.
const clientAssertionType = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"

// grantClientCredentials is the grant a node asks for its token with.
const grantClientCredentials = "client_credentials"

// assertionMaxLife is how far ahead an assertion may say it expires: its
// replay window. A node signs a fresh one for every request.
const assertionMaxLife = 5 * time.Minute

// isNodeClient is whether this client is a node. No code is ever sent to a
// node, so it has no return address.
func (c Client) isNodeClient() bool {
	return c.ReturnAddress == ""
}

// spentAssertions is every assertion id accepted, until its exp passes, so a
// captured assertion presented again is refused (RFC 7523 §3, item 7).
type spentAssertions struct {
	mu    sync.Mutex
	until map[string]time.Time
}

// spend records jti as used until exp, or says it already was.
func (s *spentAssertions) spend(jti string, exp, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.until == nil {
		s.until = map[string]time.Time{}
	}
	for id, at := range s.until {
		if now.After(at) {
			delete(s.until, id)
		}
	}
	if _, seen := s.until[jti]; seen {
		return false
	}
	s.until[jti] = exp
	return true
}

// authenticateClient is how the token endpoint learns which client asks. An
// assertion is a node signing as its did:key; anything else is a secret.
func (h *Handler) authenticateClient(ctx context.Context, r *http.Request, form url.Values) (fosite.Client, error) {
	if form.Get("client_assertion_type") == clientAssertionType {
		return h.assertedClient(form, time.Now())
	}
	provider, ok := h.oauthProvider.(*fosite.Fosite)
	if !ok {
		return nil, errors.WithStack(fosite.ErrServerError.WithHintf("the authorization server is a %T, which authenticates no secret", h.oauthProvider))
	}
	return provider.DefaultClientAuthenticationStrategy(ctx, r, form)
}

// assertedClient checks a JWT signed with EdDSA by the key the client's
// did:key names, issued by and about that client, for this token endpoint.
func (h *Handler) assertedClient(form url.Values, now time.Time) (fosite.Client, error) {
	assertion := form.Get("client_assertion")
	if assertion == "" {
		return nil, errors.WithStack(fosite.ErrInvalidRequest.WithHint("client_assertion_type was jwt-bearer and no client_assertion came with it"))
	}
	var claims jwt.RegisteredClaims
	var client Client
	_, err := jwt.ParseWithClaims(assertion, &claims, func(token *jwt.Token) (any, error) {
		subject, err := token.Claims.GetSubject()
		if err != nil || subject == "" {
			return nil, errors.New("the assertion names no subject")
		}
		if named := form.Get("client_id"); named != "" && named != subject {
			return nil, errors.Newf("the assertion is about %s and the request names %s", subject, named)
		}
		found, ok := h.clientByDID(subject)
		if !ok {
			return nil, errors.Newf("no client answers to %s", subject)
		}
		key, err := DecodeUserDID(found.DID)
		if err != nil {
			return nil, errors.Wrapf(err, "the client %s names no key", found.DID)
		}
		client = found
		return key, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
		jwt.WithAudience(h.publicOrigin()+tokenPath),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(func() time.Time { return now }),
	)
	if err != nil {
		return nil, errors.WithStack(fosite.ErrInvalidClient.WithHintf("the client assertion does not verify: %v", err))
	}
	if issuer, _ := claims.GetIssuer(); issuer != client.DID {
		return nil, errors.WithStack(fosite.ErrInvalidClient.WithHintf("the assertion is issued by %q and is about %s, and a client asserts itself", issuer, client.DID))
	}
	if claims.ExpiresAt.Time.After(now.Add(assertionMaxLife)) {
		return nil, errors.WithStack(fosite.ErrInvalidClient.WithHintf("the assertion lasts until %s, past the %s an assertion may", claims.ExpiresAt.Time.UTC().Format(time.RFC3339), assertionMaxLife))
	}
	if claims.ID == "" {
		return nil, errors.WithStack(fosite.ErrInvalidClient.WithHint("the assertion carries no jti, so it could be presented again"))
	}
	if !h.assertions.spend(claims.ID, claims.ExpiresAt.Time, now) {
		return nil, errors.WithStack(fosite.ErrJTIKnown.WithHintf("the assertion %s was already presented", claims.ID))
	}
	return clientFor(client), nil
}

// registerNode writes a node as a client of this one: POST /auth/tokens with
// level OAUTH, the node's did:key, and the one namespace it acts in.
func (h *Handler) registerNode(w http.ResponseWriter, p Presented, label, did string, level Level, namespaces []string, returnAddress, expires string) {
	if level != LevelOAuth {
		h.writeError(w, http.StatusBadRequest, "a did names a node registered as a client, which is "+string(LevelOAuth)+", and this asked for "+string(level))
		return
	}
	if returnAddress != "" {
		h.writeError(w, http.StatusBadRequest, "a node asks for its own token and is sent no code, so it has no return address")
		return
	}
	if len(namespaces) != 1 {
		h.writeError(w, http.StatusBadRequest, "a node acts in one namespace, picked at registering, and this named "+strconv.Itoa(len(namespaces)))
		return
	}
	if _, err := DecodeUserDID(did); err != nil {
		h.writeError(w, http.StatusBadRequest, "the did is not an ed25519 did:key: "+err.Error())
		return
	}
	if _, held := h.clientByDID(did); held {
		h.writeError(w, http.StatusConflict, did+" is already a client of this node")
		return
	}
	mintedBy, _ := p.Admitted()
	// Naming a namespace is crossing into one, which ADR-027 puts at SUPER.
	if namespaces[0] != NamespaceDefault && !h.stillAdmitted(mintedBy) {
		h.writeError(w, http.StatusForbidden, errRefused.Error())
		return
	}
	expiresAt, why := expiryOf(expires)
	if why != "" {
		h.writeError(w, http.StatusBadRequest, why)
		return
	}
	// The row is keyed by a hash like every token, of a value drawn and dropped: nothing presents it.
	unheld, _, err := MintToken()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "no row could be drawn for the node: "+err.Error())
		return
	}
	id, err := h.tokens.Issue(IssuedToken{
		Hash:                sha256Hex(unheld),
		DID:                 did,
		Label:               label,
		MintedBy:            mintedBy,
		MintedByUser:        p.UserID,
		MintedByDisplayName: p.DisplayName,
		Level:               LevelOAuth,
		Namespaces:          namespaces,
		ExpiresAt:           expiresAt,
	})
	if err != nil {
		h.attest(PredicateUnanswered, mintedBy, map[string]any{
			"asked": "token store", "doing": "register a node", "did": did, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "the node was not written: "+err.Error())
		return
	}
	h.attest(PredicateMinted, mintedBy, map[string]any{
		"token": id, "label": label, "level": string(LevelOAuth), "namespaces": namespaces, "did": did,
	})
	h.writeJSON(w, http.StatusOK, map[string]any{
		"id":         id,
		"label":      label,
		"did":        did,
		"minted_by":  mintedBy,
		"level":      string(LevelOAuth),
		"namespaces": namespaces,
		"expires_at": expiresAt.UTC().Format(time.RFC3339Nano),
	})
}

// issueNodeToken writes the token a node asked for. The node speaks as itself:
// it holds roles by its client's label, in the namespace its client is in.
func (s *oauthStore) issueNodeToken(signature string, session *TokenSession, client Client) error {
	// It answers to whoever registered it, the way every token answers to its minter (ADR-025).
	if !s.h.stillAdmitted(client.MintedBy) {
		return errors.WithStack(fosite.ErrAccessDenied.WithHintf("%s, who registered %s, is no longer admitted", client.MintedBy, client.DID))
	}
	until := session.GetExpiresAt(fosite.AccessToken)
	if until.IsZero() {
		return errors.WithStack(fosite.ErrServerError.WithHint("a node's token says nothing of when it ends, and was not written"))
	}
	id, err := s.h.tokens.Issue(IssuedToken{
		Hash:       signature,
		DID:        session.DID,
		Label:      client.Label,
		MintedBy:   client.MintedBy,
		Level:      LevelAttestor,
		Namespaces: []string{client.Namespace},
		ExpiresAt:  &until,
	})
	if err != nil {
		s.h.attest(PredicateUnanswered, client.MintedBy, map[string]any{
			"asked": "token store", "doing": "issue", "client": client.DID, "error": err.Error(),
		})
		return errors.WithStack(fosite.ErrServerError.WithWrap(err).WithDebug(err.Error()))
	}
	s.h.attest(PredicateMinted, client.MintedBy, map[string]any{
		"token": id, "label": client.Label, "level": string(LevelAttestor),
		"namespaces": []string{client.Namespace}, "client": client.DID, "did": session.DID,
	})
	return nil
}
