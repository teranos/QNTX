package auth

import (
	"context"
	"time"

	"github.com/ory/fosite"
	"github.com/teranos/errors"
)

// A client is a door (ADR-025). A door in am.toml is answered by the origin
// it was named after; a client is answered by its DID, and what it answers is
// where its codes go — written by the same hand at minting. returnableTo
// sends somebody only where am.toml already said; returnableToClient sends a
// code only where the record already said.

type Client struct {
	// DID is the client id: the token's own did:key.
	DID   string
	Label string
	// MintedBy is who wrote the door, the way ROOT writes one in am.toml.
	MintedBy string
	// Namespace is the door the client was minted at, which is where a token
	// issued through it acts.
	Namespace     string
	ReturnAddress string
}

// clientByDID is the live client this DID names, or false. A revoked or
// expired client is not a door, and is refused here rather than at the token
// endpoint.
//
// The list is read whole. A client is looked up once per authorize request,
// which is a person walking up, and the store is small.
func (h *Handler) clientByDID(did string) (Client, bool) {
	if h.tokens == nil || did == "" {
		return Client{}, false
	}
	listed, err := h.tokens.List()
	if err != nil {
		h.logger.Errorw("could not list tokens, so no client answers", "did", did, "error", err)
		return Client{}, false
	}
	for _, t := range listed {
		if t.DID != did || t.Level != LevelOAuth {
			continue
		}
		if t.RevokedAt != nil || expiredAt(t.ExpiresAt) {
			return Client{}, false
		}
		namespace := NamespaceDefault
		if len(t.Namespaces) > 0 {
			namespace = t.Namespaces[0]
		}
		return Client{
			DID:           t.DID,
			Label:         t.Label,
			MintedBy:      t.MintedBy,
			Namespace:     namespace,
			ReturnAddress: t.ReturnAddress,
		}, true
	}
	return Client{}, false
}

// expiredAt is whether an RFC3339 expiry has passed. An expiry that does not
// read is treated as passed: a token whose lifetime cannot be read is not one
// to send a code to.
func expiredAt(expiresAt *string) bool {
	if expiresAt == nil || *expiresAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339Nano, *expiresAt)
	if err != nil {
		return true
	}
	return !t.After(time.Now())
}

// returnableToClient is whether this client may be sent a code at this
// address: the one written on it, whole. Same rule as returnableTo, one
// record down — a place this node sends a code only if minting already said so.
func (h *Handler) returnableToClient(did, address string) (Client, bool) {
	found, ok := h.clientByDID(did)
	if !ok || address == "" || found.ReturnAddress != address {
		return Client{}, false
	}
	return found, true
}

// ClientDoors is the door lookup as fosite asks it: which client this id
// names, and where it may be sent a code.
type ClientDoors struct {
	h *Handler
}

func (h *Handler) ClientDoors() ClientDoors { return ClientDoors{h: h} }

var _ fosite.ClientManager = ClientDoors{}

// GetClient answers a live client by its DID. Its one return address is the
// only redirect it may name; a code goes to one place, and it is the place
// the record says.
//
// No secret is hashed into this record. What a client presents at the token
// endpoint is looked up by its hash the way every token is (ClientSecrets),
// so the "hashed secret" fosite hands back to be compared is the DID: the
// token the secret resolves to has to be this client.
func (d ClientDoors) GetClient(_ context.Context, id string) (fosite.Client, error) {
	found, ok := d.h.clientByDID(id)
	if !ok {
		return nil, errors.WithStack(fosite.ErrNotFound.WithHintf("no client answers to %s", id))
	}
	return clientFor(found), nil
}

// ClientSecrets is fosite's secrets hasher over the token store. A client's
// secret is the raw token it was minted as (admission.go, LevelOAuth), so
// checking it is the lookup every bearer gets: hash it, resolve it, and the
// live token it resolves to must be a client with the DID being compared.
// Revoking the client is what makes its secret stop working.
type ClientSecrets struct {
	h *Handler
}

func (h *Handler) ClientSecrets() ClientSecrets { return ClientSecrets{h: h} }

var _ fosite.Hasher = ClientSecrets{}

func (c ClientSecrets) Compare(_ context.Context, hash, secret []byte) error {
	did := string(hash)
	if c.h.tokens == nil || len(secret) == 0 {
		return errors.Newf("no secret was presented for client %s", did)
	}
	grant, ok := c.h.tokens.Lookup(sha256Hex(string(secret)))
	if !ok {
		return errors.Newf("the secret presented for client %s is not a live token", did)
	}
	if grant.Level != LevelOAuth || grant.DID != did {
		return errors.Newf("the secret presented for client %s is a %s token named %s, not this client", did, grant.Level, grant.DID)
	}
	return nil
}

// Hash is the form of a secret the store keeps, for the interface's sake.
// Nothing calls it: a client's secret is hashed where it is minted.
func (ClientSecrets) Hash(_ context.Context, secret []byte) ([]byte, error) {
	return []byte(sha256Hex(string(secret))), nil
}

// ClientAssertionJWTValid refuses: a client presents its secret, the raw
// token it was minted as, and nothing else names it.
func (ClientDoors) ClientAssertionJWTValid(_ context.Context, jti string) error {
	return errors.WithStack(fosite.ErrInvalidClient.WithHintf(
		"a client presents its secret; a JWT assertion (jti %s) does not name one", jti))
}

func (ClientDoors) SetClientAssertionJWT(_ context.Context, jti string, _ time.Time) error {
	return errors.WithStack(fosite.ErrInvalidClient.WithHintf(
		"a client presents its secret; a JWT assertion (jti %s) does not name one", jti))
}
