package auth

import (
	errors "github.com/teranos/sacred-error"
)

// "AND LAYE OWNS IDENTITY"
// "BUT LAYE DOES NOT REALLY OWN USER, THE QNTX WAY"

// LayeSays is whose a key is, by laye's record in etcd: the person whose
// passkey session said yes to it (/keys/<did>), or, for a session's key, the
// person whose key admitted it (/admitted/<did>). Found false is a key laye
// holds nothing of.
type LayeSays func(did string) (person string, found bool, err error)

// SetLayeSays gives the node laye's record of whose a key is, or none.
func (h *Handler) SetLayeSays(says LayeSays) {
	h.layeSays = says
}

// whoLayeSays is the grant a key laye carried stands on when the node holds
// none of its own for it: the person laye's record names, at the level this
// node gives that person. The person is laye's to say; what they may do here
// is the node's, as the device flow decides it (device.go): a person this
// node no longer admits is nobody.
func (h *Handler) whoLayeSays(did string) (TokenInfo, error) {
	if h.layeSays == nil {
		return TokenInfo{}, errors.New("this node reads no record of laye's")
	}
	person, found, err := h.layeSays(did)
	if err != nil {
		return TokenInfo{}, errors.Wrapf(err, "laye's record of %s was not read", did)
	}
	if !found {
		return TokenInfo{}, errors.Newf("laye holds no person for %s", did)
	}
	level := h.levelOf(person)
	if level == "" {
		return TokenInfo{}, errors.Newf("laye says %s is %s, and this node admits %s at no level", did, person, person)
	}
	user, _, err := h.users.ByRoute(person)
	if err != nil {
		return TokenInfo{}, errors.Wrapf(err, "the User laye's %s reaches was not read", person)
	}
	return TokenInfo{
		Label:               "laye",
		DID:                 did,
		MintedBy:            person,
		MintedByUser:        user.ID,
		MintedByDisplayName: user.Name(),
		Level:               level,
		ClientDID:           did,
	}, nil
}
