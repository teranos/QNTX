package auth

import (
	errors "github.com/teranos/sacred-error"
)

// "Why wouldn't the phone be one in the same way ? The App"
//
// k is a client of q: it signs as its own did:key, and q issues it a token for
// the person who said yes at the passkey. The App holds a laye key the same
// way. The passkey at home enrols it: the key the half-admission was proved
// with, sent home from an app's door, is written on the User who holds it as
// a client at that door. From then on the App is sent a code at its door and
// spends it signing as its key, the way k does.

// appClient is the App a person's key signs in as, or false. The key is the
// client: a User letting go of it, or am.toml striking the door it was
// enrolled at, is the client gone.
func (h *Handler) appClient(did string) (Client, bool) {
	u, found, err := h.users.ByRoute(did)
	if err != nil {
		h.logger.Errorw("could not read the User a key reaches, so no App answers to it", "did", did, "error", err)
		return Client{}, false
	}
	if !found {
		return Client{}, false
	}
	// A key enrolled at no door is at a door nothing opens, and is no client.
	for _, k := range u.Keys {
		if k.DID != did {
			continue
		}
		opened, open := h.doors.at(k.Door)
		if !open {
			return Client{}, false
		}
		return Client{
			DID:           did,
			Label:         k.Door,
			Namespace:     opened.namespace,
			ReturnAddress: k.Door,
			App:           true,
		}, true
	}
	return Client{}, false
}

// enrolApp writes the key laye proved the App with as a client at the door the
// way home began at. Only an app's door, and only a key the person already
// holds: a browser tab is reached by no scheme, and a key the User does not
// hold is somebody else's. A passkey that stood on a session proved no key,
// and no key the User holds is that one.
func (h *Handler) enrolApp(identity, door, layeKey string) error {
	if isWebOrigin(door) {
		return nil
	}
	u, found, err := h.users.ByRoute(identity)
	if err != nil {
		return errors.Wrapf(err, "failed to read the User %s reaches, to enrol the App at %s", quoteIdentity(identity), door)
	}
	if !found {
		return nil
	}
	for i, k := range u.Keys {
		if k.DID != layeKey {
			continue
		}
		if k.Door == door {
			return nil
		}
		keys := append([]UserKey(nil), u.Keys...)
		keys[i].Door = door
		u.Keys = keys
		if err := h.users.Put(u); err != nil {
			return errors.Wrapf(err, "failed to enrol %s as the App at %s for User %s", layeKey, door, u.ID)
		}
		h.attest(PredicateEnrolled, identity, map[string]any{"client": layeKey, "door": door})
		h.logger.Infow("The passkey at home enrolled the App", "admitted_as", identity, "client", layeKey, "door", door)
		return nil
	}
	return nil
}
