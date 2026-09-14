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

// Client is a client as the door lookup answers it.
type Client struct {
	// DID is the client id: the token's own did:key.
	DID   string
	Label string
	// MintedBy is who wrote the door, the way ROOT writes one in am.toml.
	MintedBy string
	// Namespace is the door the client was minted at, which is where a token
	// issued through it acts.
	Namespace string
	// ReturnAddress is where its codes go, whole.
	ReturnAddress string
}

// clientByDID is the live client this DID names, or false. A revoked or
// expired client is not a door: a code sent to it would open nothing, so it
// is refused here rather than at the token endpoint.
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
		if t.DID != did || t.Level != LevelClient {
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

// ClientDoors is what fosite is handed for its client manager.
func (h *Handler) ClientDoors() ClientDoors { return ClientDoors{h: h} }

var _ fosite.ClientManager = ClientDoors{}

// GetClient answers a live client by its DID. Its one return address is the
// only redirect it may name; a code goes to one place, and it is the place
// the record says.
//
// No secret is hashed into this record. What a client presents at the token
// endpoint is looked up by its hash the way every token is, which the token
// endpoint does when it exists.
func (d ClientDoors) GetClient(_ context.Context, id string) (fosite.Client, error) {
	found, ok := d.h.clientByDID(id)
	if !ok {
		return nil, errors.WithStack(fosite.ErrNotFound.WithHintf("no client answers to %s", id))
	}
	return &fosite.DefaultClient{
		ID:            found.DID,
		RedirectURIs:  []string{found.ReturnAddress},
		GrantTypes:    []string{"authorization_code", "refresh_token"},
		ResponseTypes: []string{"code"},
	}, nil
}

// ClientAssertionJWTValid refuses: a client presents its secret, the raw
// token it was minted as, and nothing else names it.
func (ClientDoors) ClientAssertionJWTValid(_ context.Context, jti string) error {
	return errors.WithStack(fosite.ErrInvalidClient.WithHintf(
		"a client presents its secret; a JWT assertion (jti %s) does not name one", jti))
}

// SetClientAssertionJWT refuses for the same reason.
func (ClientDoors) SetClientAssertionJWT(_ context.Context, jti string, _ time.Time) error {
	return errors.WithStack(fosite.ErrInvalidClient.WithHintf(
		"a client presents its secret; a JWT assertion (jti %s) does not name one", jti))
}
