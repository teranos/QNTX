package auth

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ory/fosite"
	errors "github.com/teranos/sacred-error"
)

// A node is a client of another node, registered by its own did:key. It
// proves itself by signing with that key (RFC 7523 §2.2), so no secret exists.

// "every QNTX node is an OAuth server and an OAuth client. Nodes authenticate
// to each other only through OAuth, over the public internet, with no network trusted"

// nodesPath is where ROOT registers another node as a client of this one.
const nodesPath = "/auth/nodes"

// clientAssertionType is RFC 7523 §2.2's client_assertion_type.
const clientAssertionType = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"

// grantClientCredentials is the grant a node asks for its token with.
const grantClientCredentials = "client_credentials"

// assertionMaxLife is how far ahead an assertion may say it expires: its
// replay window. A node signs a fresh one for every request.
const assertionMaxLife = 5 * time.Minute

// assertionIDBytes is a node's assertion id: random bytes, hex, so one id is never drawn twice.
const assertionIDBytes = 16

// spentAssertions is every assertion id accepted, until its exp passes, so a
// captured assertion presented again is refused (RFC 7523 §3, item 7).
type spentAssertions struct {
	until sync.Map
}

// spend records jti as used until exp. A jti already held is answered with
// when it was kept until, and is not fresh.
func (s *spentAssertions) spend(jti string, exp, now time.Time) (any, bool) {
	s.until.Range(func(id, at any) bool {
		if kept, isTime := at.(time.Time); !isTime || now.After(kept) {
			s.until.Delete(id)
		}
		return true
	})
	prior, loaded := s.until.LoadOrStore(jti, exp)
	return prior, !loaded
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

// assertedClient checks a JWT signed with EdDSA by the key a node's did:key
// names, issued by and about that node, for this token endpoint.
func (h *Handler) assertedClient(form url.Values, now time.Time) (fosite.Client, error) {
	var claims jwt.RegisteredClaims
	var client Client
	parsed, err := jwt.ParseWithClaims(form.Get("client_assertion"), &claims, func(token *jwt.Token) (any, error) {
		subject, err := token.Claims.GetSubject()
		if err != nil {
			return nil, errors.Wrap(err, "the assertion's subject does not read")
		}
		if form.Get("client_id") != subject {
			return nil, errors.Newf("the assertion is about %q and the request names %q", subject, form.Get("client_id"))
		}
		found, ok := h.clientByDID(subject)
		if !ok {
			return nil, errors.Newf("no client answers to %q", subject)
		}
		if !found.Node {
			return nil, errors.Newf("%s is sent codes; only a node signs as its key", found.DID)
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
	if !parsed.Valid {
		return nil, errors.WithStack(fosite.ErrInvalidClient.WithHint("the client assertion does not verify"))
	}
	issuer, err := claims.GetIssuer()
	if err != nil {
		return nil, errors.WithStack(fosite.ErrInvalidClient.WithHintf("the assertion's issuer does not read: %v", err))
	}
	if issuer != client.DID {
		return nil, errors.WithStack(fosite.ErrInvalidClient.WithHintf("the assertion is issued by %q and is about %s, and a node asserts itself", issuer, client.DID))
	}
	if claims.ExpiresAt.Time.After(now.Add(assertionMaxLife)) {
		return nil, errors.WithStack(fosite.ErrInvalidClient.WithHintf("the assertion lasts until %s, past the %s an assertion may", claims.ExpiresAt.Time.UTC().Format(time.RFC3339), assertionMaxLife))
	}
	id, err := hex.DecodeString(claims.ID)
	if err != nil || len(id) != assertionIDBytes {
		return nil, errors.WithStack(fosite.ErrInvalidClient.WithHintf("the assertion's jti %q is not %d random bytes in hex, so it could be drawn again", claims.ID, assertionIDBytes))
	}
	if prior, fresh := h.assertions.spend(claims.ID, claims.ExpiresAt.Time, now); !fresh {
		return nil, errors.WithStack(fosite.ErrJTIKnown.WithHintf("the assertion %s was already presented, and is kept until %v", claims.ID, prior))
	}
	return clientFor(client), nil
}

// names is whether a word names something: it has a letter in it.
func names(word string) bool {
	return strings.IndexFunc(word, unicode.IsLetter) >= 0
}

// handleRegisterNode writes another node as a client of this one, behind a
// session. POST /auth/nodes {"label", "did", "namespace", "return_address",
// "expires_at"}. The return address is the node's own way back for the people
// it sends here to log in.
func (h *Handler) handleRegisterNode(w http.ResponseWriter, r *http.Request, p Presented) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Label         string `json:"label"`
		DID           string `json:"did"`
		Namespace     string `json:"namespace"`
		ReturnAddress string `json:"return_address"`
		ExpiresAt     string `json:"expires_at"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxCeremonyBodyBytes)).Decode(&req); err != nil {
		h.writeError(w, http.StatusBadRequest, "the body did not parse as JSON: "+err.Error())
		return
	}
	label, did, namespace := strings.TrimSpace(req.Label), strings.TrimSpace(req.DID), strings.TrimSpace(req.Namespace)
	if !names(label) {
		h.writeError(w, http.StatusBadRequest, "a node is registered under a label, which roles are granted to, and this named "+label)
		return
	}
	if !names(namespace) {
		h.writeError(w, http.StatusBadRequest, "a node acts in the one namespace it is registered in, and this named "+namespace)
		return
	}
	returnAddress := strings.TrimSpace(req.ReturnAddress)
	if err := returnable(returnAddress); err != nil {
		h.writeError(w, http.StatusBadRequest, "a node is sent back the people it sends here: "+err.Error())
		return
	}
	key, err := DecodeUserDID(did)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, "the did is not an ed25519 did:key: "+err.Error())
		return
	}
	if len(key) != ed25519.PublicKeySize {
		h.writeError(w, http.StatusBadRequest, "the did names a key that is not an ed25519 public key")
		return
	}
	if held, already := h.clientByDID(did); already {
		h.writeError(w, http.StatusConflict, did+" is already a client of this node, as "+held.Label)
		return
	}
	listed, err := h.tokens.List()
	if err != nil {
		h.writeError(w, http.StatusInternalServerError, "the token store did not answer: "+err.Error())
		return
	}
	for _, other := range listed {
		if other.Label == label {
			h.writeError(w, http.StatusConflict, "a token is already named "+label+": "+other.ID)
			return
		}
	}
	mintedBy, admitted := p.Admitted()
	if !admitted {
		h.writeError(w, http.StatusUnauthorized, "no session")
		return
	}
	expiresAt, err := expiryOf(req.ExpiresAt)
	if err != nil {
		h.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The row is keyed by a hash like every token, of bytes drawn and dropped: nothing presents it.
	unheld := make([]byte, tokenSeedBytes)
	drawn, err := rand.Read(unheld)
	if err != nil || drawn != tokenSeedBytes {
		h.writeError(w, http.StatusInternalServerError, "no row could be drawn for the node")
		return
	}
	id, err := h.tokens.Issue(IssuedToken{
		Hash:                sha256Hex(hex.EncodeToString(unheld)),
		DID:                 did,
		Label:               label,
		MintedBy:            mintedBy,
		MintedByUser:        p.UserID,
		MintedByDisplayName: p.DisplayName,
		Level:               LevelNode,
		Namespaces:          []string{namespace},
		ExpiresAt:           expiresAt,
		ReturnAddress:       returnAddress,
	})
	if err != nil {
		h.attest(PredicateUnanswered, mintedBy, map[string]any{
			"asked": "token store", "doing": "register a node", "did": did, "error": err.Error(),
		})
		h.writeError(w, http.StatusInternalServerError, "the node was not written: "+err.Error())
		return
	}
	h.attest(PredicateMinted, mintedBy, map[string]any{
		"token": id, "label": label, "level": string(LevelNode), "namespaces": []string{namespace}, "did": did,
	})
	h.writeJSON(w, http.StatusOK, map[string]any{
		"id":         id,
		"label":      label,
		"did":        did,
		"minted_by":  mintedBy,
		"level":      string(LevelNode),
		"namespaces": []string{namespace},
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
