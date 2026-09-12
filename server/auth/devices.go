package auth

import "github.com/go-webauthn/webauthn/webauthn"

// A device belongs to the person, not to the route they came in by.

// auth.root_identities lists ways to reach a User, not Users (ADR-031), and
// an authenticator derives one key per device the User holds. A laptop
// enrolled under the google route and a phone under the apple route are two
// devices of one person, so whichever logo they press, both are theirs to
// assert and neither is somebody else's.
//
// "Both are apple, I use a MacBook Pro." "Should be same identity, same user though."

// routesOf is every route reaching the User this route reaches, the route
// itself first. A deployment keeping no Users, or a route no User holds yet,
// is the route alone: the first login has nobody to widen to.
func (h *Handler) routesOf(route string) []string {
	routes := []string{route}
	if h.users == nil {
		return routes
	}
	u, found, err := h.users.ByRoute(route)
	if err != nil {
		h.logger.Errorw("could not read the User a route reaches; its devices are the route's own",
			"route", route, "error", err)
		return routes
	}
	if !found {
		return routes
	}
	for _, a := range u.Accounts {
		if a.CanonicalID != route {
			routes = append(routes, a.CanonicalID)
		}
	}
	for _, k := range u.Keys {
		if k.DID != route {
			routes = append(routes, k.DID)
		}
	}
	return routes
}

// devicesOf is what a login admitted by this route may assert: the devices of
// the person it reaches, made at this door.
func (h *Handler) devicesOf(door, route string) ([]webauthn.Credential, error) {
	return h.creds.doorCredentialsFor(door, h.routesOf(route))
}

// hasDevice reports whether the person this route reaches has stood on a
// device anywhere. It decides whether a login is asked to assert or to enrol.
func (h *Handler) hasDevice(route string) (bool, error) {
	return h.creds.existsForAny(h.routesOf(route))
}
