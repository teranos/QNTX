package auth

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/golang-jwt/jwt/v5"
	"github.com/ory/fosite"
	"github.com/teranos/QNTX/internal/named"
	"github.com/teranos/QNTX/internal/slug"
	errors "github.com/teranos/sacred-error"
)

// A peer is a client of the primary, declared in nodes.peers by its own
// did:key. It proves itself by signing with that key (RFC 7523 §2.2), so no
// secret exists.

// "every QNTX node is an OAuth server and an OAuth client. Nodes authenticate
// to each other only through OAuth, over the public internet, with no network trusted"

// clientAssertionType is RFC 7523 §2.2's client_assertion_type.
const clientAssertionType = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"

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
		if !found.Node && !found.App {
			return nil, errors.Newf("%s is sent codes; only a node or an App signs as its key", found.DID)
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

// The primary keeps where every namespace lives: at the peer nodes.peers
// declares serving it, or here.

// DeclarePeer makes a peer a client of this node, as nodes.peers names it:
// what it is called here, its did:key, the namespaces it serves, the origin it
// answers at, and the origin its people's browsers come back to.
func (h *Handler) DeclarePeer(name, did string, namespaces []string, origin, returnAddress string) error {
	name, did = strings.TrimSpace(name), strings.TrimSpace(did)
	if !names(name) || strings.ContainsAny(name, "/?#") {
		return errors.Newf("nodes.peers names %s as %q, which is no name a path can carry", did, name)
	}
	// What it is told about a token is whether it acts there, so a peer
	// declared without one would be told about none.
	declared, err := named.ItemsOf("nodes.peers, for the namespaces "+did+" serves,", namespaces)
	if err != nil {
		return err
	}
	served := make([]string, 0, len(declared.All()))
	for _, written := range declared.All() {
		namespace := strings.TrimSpace(written)
		if !names(namespace) {
			return errors.Newf("nodes.peers names %s serving %q, which names no namespace", did, written)
		}
		if slices.ContainsFunc(served, sameNamespace(namespace)) {
			return errors.Newf("nodes.peers names %s serving %s twice", did, namespace)
		}
		// A namespace lives at one peer.
		if home, lives := h.HomeOf(namespace); lives {
			return errors.Newf("nodes.peers names %s and %s both serving %s, and a namespace lives at one", home.DID, did, namespace)
		}
		served = append(served, namespace)
	}
	key, err := DecodeUserDID(did)
	if err != nil {
		return errors.Wrapf(err, "nodes.peers names %q, which is not an ed25519 did:key", did)
	}
	if len(key) != ed25519.PublicKeySize {
		return errors.Newf("nodes.peers names %q, whose key is not an ed25519 public key", did)
	}
	parsed, err := peerOrigin(did, "origin", origin)
	if err != nil {
		return err
	}
	// A peer only its own machine opens is reached here over a tunnel, and by
	// its browser at a loopback host: the one plain http a code may be sent to.
	returnTo, err := peerOrigin(did, "return_address", returnAddress)
	if err != nil {
		return err
	}
	if _, held := h.nodes[did]; held {
		return errors.Newf("nodes.peers names %s twice", did)
	}
	if held, named := h.peerNamed(name); named {
		return errors.Newf("nodes.peers names %s and %s both %s", held.DID, did, name)
	}
	peers := map[string]Client{did: {
		DID:           did,
		Label:         name,
		Namespaces:    served,
		ReturnAddress: returnTo.String() + elsewhereBackPath,
		Origin:        parsed,
		Node:          true,
	}}
	for held, peer := range h.nodes {
		peers[held] = peer
	}
	h.nodes = peers
	return nil
}

// peerOrigin reads one of a peer's origins as nodes.peers wrote it: a scheme
// and a host, nothing after.
func peerOrigin(did, field, written string) (*url.URL, error) {
	origin := strings.TrimSuffix(strings.TrimSpace(written), "/")
	parsed, err := url.Parse(origin)
	if err != nil || !parsed.IsAbs() || parsed.Scheme+"://"+parsed.Host != origin || !names(parsed.Scheme) {
		return nil, errors.Newf("nodes.peers names %s with %s %q, which is not an origin such as https://node.example.com", did, field, written)
	}
	return parsed, nil
}

// peerNamed is the peer nodes.peers calls name, or false.
func (h *Handler) peerNamed(name string) (Client, bool) {
	for _, peer := range h.nodes {
		if peer.Label == name {
			return peer, true
		}
	}
	return Client{}, false
}

// HomeOf is the peer a namespace lives at, as nodes.peers declares it, or
// false for one no peer serves.
func (h *Handler) HomeOf(namespace string) (Client, bool) {
	for _, peer := range h.nodes {
		if peer.Serves(namespace) {
			return peer, true
		}
	}
	return Client{}, false
}

// Serves is whether a client serves namespace: a peer, one of the namespaces
// nodes.peers declares it serving; any other client, the door it was minted at.
func (c Client) Serves(namespace string) bool {
	return slices.ContainsFunc(c.Namespaces, sameNamespace(namespace))
}

// sameNamespace is whether a name is namespace, as a door reaches it: by slug.
func sameNamespace(namespace string) func(string) bool {
	return func(named string) bool { return slug.Of(named) == slug.Of(namespace) }
}
