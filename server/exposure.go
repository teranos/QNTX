package server

import (
	"net/url"

	appcfg "github.com/teranos/QNTX/internal/config"
	errors "github.com/teranos/sacred-error"
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
// network.
func offLoopback(bindAddr string) bool {
	return !appcfg.IsLoopbackAddress(bindAddr)
}

// unsaidOnEveryNode is what a node runs with whatever else it runs, and so
// must say: "absence is absence", and a limit left out is not a limit.
var unsaidOnEveryNode = []string{
	// Whether a gate stands in front of the node is said, true or false, and
	// leaving it out does not open one.
	"auth.enabled",
	"storage.backend",
	"server.port",
	"server.bind_address",
	// "Pulse runs because the node runs."
	"pulse.workers",
	"pulse.ticker_interval_seconds",
	"server.rate_limit.auth_rate", "server.rate_limit.auth_burst",
	"server.rate_limit.ws_rate", "server.rate_limit.ws_burst",
	"server.rate_limit.write_rate", "server.rate_limit.write_burst",
	"server.rate_limit.read_rate", "server.rate_limit.read_burst",
	"server.rate_limit.public_rate", "server.rate_limit.public_burst",
	"server.rate_limit.staand_rate", "server.rate_limit.staand_burst",
	"server.rate_limit.opened_rate", "server.rate_limit.opened_burst",
	"llm.max_concurrent", "llm.max_calls_per_minute", "llm.max_queue_depth", "llm.cooldown_seconds",
	"fetch.max_requests_per_window", "fetch.window_seconds",
	"watcher.max_fires_per_second",
	// An embedding service arrives with a plugin, at runtime, and is tuned by
	// what the node was started with.
	"embeddings.cluster_threshold", "embeddings.min_cluster_size",
	"embeddings.cluster_match_threshold", "embeddings.projection_methods",
}

// RefuseUnsaid refuses a node whose config left out what it runs with, and
// names every key left out at once, so one edit to am.toml answers it. Asked
// by `qntx server` before it serves.
func RefuseUnsaid(cfg *appcfg.Config) error {
	keys := append([]string(nil), unsaidOnEveryNode...)
	if cfg.Auth.Enabled {
		keys = append(keys, "auth.session_expiry_hours")
	}
	if cfg.Said("sentry.dsn") {
		keys = append(keys, "sentry.min_level", "sentry.capture_errors", "sentry.flush_seconds", "sentry.debug")
	}
	if cfg.Said("distill.interval_seconds") {
		keys = append(keys, "distill.max_age_hours", "distill.batch_size")
	}
	return cfg.RefuseUnsaid("the node does not run on what it was not told", keys...)
}

// refusePublicDeploy names what a deployment on this bind address has not said
// yet. A non-nil return stops startup. Loopback answers nil to all of it.
func refusePublicDeploy(bindAddr string, auth appcfg.AuthConfig) error {
	if !offLoopback(bindAddr) {
		return nil
	}
	if quickdev {
		return errors.Newf(
			"QuickDev is for developing against, so it binds loopback only, not server.bind_address %q",
			bindAddr,
		)
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
