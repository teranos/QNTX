package config

import (
	"strings"

	"github.com/teranos/QNTX/internal/secretref"
	"github.com/teranos/errors"
)

// KnownStorageBackends is the set of accepted values for [storage] backend.
// See ADR-023 (backend selection) and ADR-024 (parquet backend).
var KnownStorageBackends = map[string]bool{
	"sqlite":  true,
	"parquet": true,
}

// KnownSentryLevels is the set of accepted values for [sentry] min_level.
// zapcore parses more names than these; the ones here are the ones a node has
// levels for, so a value it would accept and never emit is refused instead.
var KnownSentryLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

// Validate checks that the configuration is valid
func (c *Config) Validate() error {
	// Storage backend must be one of the known values (ADR-023).
	if !KnownStorageBackends[c.Storage.Backend] {
		return errors.Newf("storage.backend must be one of [sqlite, parquet], got %q", c.Storage.Backend)
	}

	// Parquet backend requires a location URL (ADR-024).
	if c.Storage.Backend == "parquet" {
		loc := c.Storage.Parquet.Location
		if loc == "" {
			return errors.New("storage.parquet.location is required when storage.backend = \"parquet\"")
		}
		if !strings.HasPrefix(loc, "s3://") && !strings.HasPrefix(loc, "file://") {
			return errors.Newf("storage.parquet.location must start with s3:// or file://, got %q", loc)
		}
	}

	// Server port: 0 is invalid (omit for default), negative is invalid
	if c.Server.Port != nil && *c.Server.Port == 0 {
		return errors.New("server.port cannot be 0 (omit for default port 8770)")
	}
	if c.Server.Port != nil && *c.Server.Port < 0 {
		return errors.Newf("server.port must be positive, got %d", *c.Server.Port)
	}

	// Pulse workers: 0 = no background workers, negative = invalid
	if c.Pulse.Workers < 0 {
		return errors.Newf("pulse.workers must be >= 0, got %d", c.Pulse.Workers)
	}

	// Pulse ticker interval: 0 = no periodic ticking, negative = invalid
	if c.Pulse.TickerIntervalSeconds < 0 {
		return errors.Newf("pulse.ticker_interval_seconds must be >= 0, got %d", c.Pulse.TickerIntervalSeconds)
	}

	// Budget values: 0 = no budget (valid per "zero means zero"), negative = invalid
	if c.Pulse.DailyBudgetUSD < 0 {
		return errors.Newf("pulse.daily_budget_usd must be >= 0, got %f", c.Pulse.DailyBudgetUSD)
	}
	if c.Pulse.WeeklyBudgetUSD < 0 {
		return errors.Newf("pulse.weekly_budget_usd must be >= 0, got %f", c.Pulse.WeeklyBudgetUSD)
	}
	if c.Pulse.MonthlyBudgetUSD < 0 {
		return errors.Newf("pulse.monthly_budget_usd must be >= 0, got %f", c.Pulse.MonthlyBudgetUSD)
	}
	if c.Pulse.CostPerScoreUSD < 0 {
		return errors.Newf("pulse.cost_per_score_usd must be >= 0, got %f", c.Pulse.CostPerScoreUSD)
	}

	// Plugin access tokens are references, never secrets. am.toml ships as a
	// world-readable SSM String parameter, so a literal here is already leaked.
	for i, entry := range c.Plugin.AccessToken {
		if entry.Host == "" {
			return errors.Newf("plugin.access_token[%d] has no host", i)
		}
		if err := secretref.Validate(entry.Ref); err != nil {
			return errors.Wrapf(err, "plugin.access_token for host %q is invalid", entry.Host)
		}
	}

	// The OAuth clients, for the same reason as the access tokens above: the
	// secret is a reference or it is disclosed. Half a client is a provider that
	// is drawn and then fails, so it is refused at load instead.
	//
	// A door's own clients are the same shape and get the same reading. A door
	// naming none is the ordinary case and falls back to the node's.
	if err := validateProviders("auth.provider", c.Auth.Provider); err != nil {
		return err
	}
	for namespace, configured := range c.Auth.Door {
		if err := validateProviders("auth.door."+namespace+".provider", configured.Provider); err != nil {
			return err
		}
	}

	// Plugin keepalive: validate when enabled (nil = default, 0 is invalid per "zero means zero")
	if c.Plugin.WebSocket.Keepalive.Enabled {
		if c.Plugin.WebSocket.Keepalive.PingIntervalSecs != nil && *c.Plugin.WebSocket.Keepalive.PingIntervalSecs <= 0 {
			return errors.Newf("plugin.websocket.keepalive.ping_interval_secs must be > 0, got %d (omit for default)", *c.Plugin.WebSocket.Keepalive.PingIntervalSecs)
		}
		if c.Plugin.WebSocket.Keepalive.PongTimeoutSecs != nil && *c.Plugin.WebSocket.Keepalive.PongTimeoutSecs <= 0 {
			return errors.Newf("plugin.websocket.keepalive.pong_timeout_secs must be > 0, got %d (omit for default)", *c.Plugin.WebSocket.Keepalive.PongTimeoutSecs)
		}
		if c.Plugin.WebSocket.Keepalive.ReconnectAttempts != nil && *c.Plugin.WebSocket.Keepalive.ReconnectAttempts < 0 {
			return errors.Newf("plugin.websocket.keepalive.reconnect_attempts must be >= 0, got %d (omit for default)", *c.Plugin.WebSocket.Keepalive.ReconnectAttempts)
		}
	}

	// Bounded storage limits: must be positive (omit for defaults: 32/64/64)
	if c.Storage.Sqlite.BoundedStorage.ActorContextLimit <= 0 {
		return errors.Newf("storage.sqlite.bounded_storage.actor_context_limit must be > 0, got %d (omit for default 32)", c.Storage.Sqlite.BoundedStorage.ActorContextLimit)
	}
	if c.Storage.Sqlite.BoundedStorage.ActorContextsLimit <= 0 {
		return errors.Newf("storage.sqlite.bounded_storage.actor_contexts_limit must be > 0, got %d (omit for default 64)", c.Storage.Sqlite.BoundedStorage.ActorContextsLimit)
	}
	if c.Storage.Sqlite.BoundedStorage.EntityActorsLimit <= 0 {
		return errors.Newf("storage.sqlite.bounded_storage.entity_actors_limit must be > 0, got %d (omit for default 64)", c.Storage.Sqlite.BoundedStorage.EntityActorsLimit)
	}

	// Embeddings intervals: nil = not scheduled (default), must be positive when set
	if c.Embeddings.ReclusterIntervalSeconds != nil && *c.Embeddings.ReclusterIntervalSeconds <= 0 {
		return errors.Newf("embeddings.recluster_interval_seconds must be > 0 when set, got %d (omit to disable)", *c.Embeddings.ReclusterIntervalSeconds)
	}
	if c.Embeddings.ReprojectIntervalSeconds != nil && *c.Embeddings.ReprojectIntervalSeconds <= 0 {
		return errors.Newf("embeddings.reproject_interval_seconds must be > 0 when set, got %d (omit to disable)", *c.Embeddings.ReprojectIntervalSeconds)
	}
	if c.Embeddings.ClusterLabelIntervalSeconds != nil && *c.Embeddings.ClusterLabelIntervalSeconds <= 0 {
		return errors.Newf("embeddings.cluster_label_interval_seconds must be > 0 when set, got %d (omit to disable)", *c.Embeddings.ClusterLabelIntervalSeconds)
	}

	// Front doors. A door that cannot work is refused where am.toml is
	// read, rather than at the moment somebody arrives at it. Whether the rp id
	// covers its origins is the browser's rule and is checked where the relying
	// party is built, so it is asked once and not restated here.
	for namespace, configuredDoor := range c.Auth.Door {
		if namespace == "" {
			return errors.New("auth.door has an entry with no namespace, which is a door onto nothing")
		}
		if configuredDoor.RPID == "" {
			return errors.Newf("auth.door.%s needs an rp_id — a door is a relying party of its own", namespace)
		}
		if len(configuredDoor.Origins) == 0 {
			return errors.Newf("auth.door.%s names no origins, so nothing reaches it", namespace)
		}
	}

	// Sentry: an unreadable min_level would silently ship nothing or everything,
	// and either one is found out later, off the box. It is refused at load.
	if c.Sentry.DSN != "" {
		if !KnownSentryLevels[c.Sentry.MinLevel] {
			return errors.Newf("sentry.min_level must be one of [debug, info, warn, error], got %q", c.Sentry.MinLevel)
		}
		if !strings.HasPrefix(c.Sentry.DSN, "https://") && !strings.HasPrefix(c.Sentry.DSN, "http://") {
			return errors.New("sentry.dsn must be the ingest URL Sentry gives for the project")
		}
		if c.Sentry.FlushSeconds < 0 {
			return errors.Newf("sentry.flush_seconds must be >= 0, got %d", c.Sentry.FlushSeconds)
		}
	}

	return nil
}

// validateProviders reads one set of operator-registered OAuth clients. The
// node has one set and every door may have its own, so where they are found
// is the only thing that differs and `at` is what says which.
func validateProviders(at string, p ProviderConfig) error {
	if err := validateOAuthClient(at+".google", p.Google.ClientID, p.Google.ClientSecretRef); err != nil {
		return err
	}
	return validateAppleClient(at+".apple", p.Apple)
}

// validateAppleClient holds Apple to all four or nothing. The secret is minted
// from the team, the key id and the key, so any one missing is a provider
// that gets drawn and then cannot sign.
func validateAppleClient(at string, a AppleClientConfig) error {
	given := 0
	for _, field := range []string{a.ClientID, a.TeamID, a.KeyID, a.PrivateKeyRef} {
		if field != "" {
			given++
		}
	}
	if given != 0 && given != 4 {
		return errors.Newf("%s needs client_id, team_id, key_id and private_key together, or none of them", at)
	}
	if err := secretref.Validate(a.PrivateKeyRef); err != nil {
		return errors.Wrapf(err, "%s.private_key is invalid", at)
	}
	return nil
}

func validateOAuthClient(at, clientID, secretRef string) error {
	if (clientID == "") != (secretRef == "") {
		return errors.Newf("%s needs both client_id and client_secret, or neither", at)
	}
	if err := secretref.Validate(secretRef); err != nil {
		return errors.Wrapf(err, "%s.client_secret is invalid", at)
	}
	return nil
}
