package server

import (
	"net/url"

	appcfg "github.com/teranos/QNTX/internal/config"
	"github.com/teranos/errors"
)

// servedOverTLS reports whether a browser reaches this deployment over TLS.
// auth.rp_origins is the origins the browser presents; the bind address is the
// socket, and a reverse proxy terminating TLS puts those two at odds.
// An entry that cannot be parsed refuses rather than being skipped: skipping
// the only https entry would silently drop Secure from every auth cookie.
func servedOverTLS(rpOrigins []string) (bool, error) {
	for _, origin := range rpOrigins {
		parsed, err := url.Parse(origin)
		if err != nil {
			return false, errors.Wrapf(err, "auth.rp_origins entry %q is not a URL", origin)
		}
		// One https entry is enough: reading a mixed list as plain http would
		// drop Secure on the origin that needs it.
		if parsed.Scheme == "https" {
			return true, nil
		}
	}
	return false, nil
}

// offLoopback reports whether this bind address puts every endpoint on the
// network. An empty address is 127.0.0.1, which is what the server binds to.
func offLoopback(bindAddr string) bool {
	if bindAddr == "" {
		bindAddr = "127.0.0.1"
	}
	return !appcfg.IsLoopbackAddress(bindAddr)
}

// refusePublicDeploy names what a deployment on this bind address has not said
// yet. A non-nil return stops startup. Loopback answers nil to all of it.
func refusePublicDeploy(bindAddr string, auth appcfg.AuthConfig) error {
	if !offLoopback(bindAddr) {
		return nil
	}
	if !auth.Enabled {
		return errors.Newf(
			"auth.enabled must be true when server.bind_address is %q (non-loopback bind exposes all endpoints to the network)",
			bindAddr,
		)
	}
	if auth.RPID == "" {
		return errors.Newf(
			"auth.rp_id must be set when server.bind_address is %q and auth.enabled is true (WebAuthn RPID cannot fall back to \"localhost\" for a non-loopback bind — browsers will reject any passkey ceremony)",
			bindAddr,
		)
	}
	// Without a list, mayRegister falls to the ungoverned path and enrolment is
	// gated on there being no credential yet rather than on who is asking.
	if len(auth.RootIdentities) == 0 {
		return errors.Newf(
			"auth.root_identities must name at least one identity when server.bind_address is %q and auth.enabled is true (ADR-030)",
			bindAddr,
		)
	}
	// Unset, a ceremony's redirect_uri is loopback, which no provider off this
	// machine can deliver to.
	if auth.PublicOrigin == "" {
		return errors.Newf(
			"auth.public_origin must name where this node answers when server.bind_address is %q (a provider ceremony's redirect_uri is built from it)",
			bindAddr,
		)
	}
	return nil
}
