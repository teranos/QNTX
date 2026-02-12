package am

import "github.com/teranos/QNTX/errors"

// Validate checks that the configuration is valid
func (c *Config) Validate() error {
	// Database path is optional - empty defaults to "qntx.db" per defaults.go:11
	// No validation needed here

	// Server port: 0 is invalid (omit for default), negative is invalid
	if c.Server.Port != nil && *c.Server.Port == 0 {
		return errors.New("server.port cannot be 0 (omit for default port 877)")
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

	// Validate local inference configuration only when enabled
	if c.LocalInference.Enabled {
		if c.LocalInference.BaseURL == "" {
			return errors.New("local_inference.base_url cannot be empty when enabled")
		}
		if c.LocalInference.Model == "" {
			return errors.New("local_inference.model cannot be empty when enabled")
		}
		if c.LocalInference.TimeoutSeconds <= 0 {
			return errors.Newf("local_inference.timeout_seconds must be > 0, got %d", c.LocalInference.TimeoutSeconds)
		}
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

	// Bounded storage limits: 0 = use default (per struct docs), negative = invalid
	if c.Database.BoundedStorage.ActorContextLimit < 0 {
		return errors.Newf("database.bounded_storage.actor_context_limit must be >= 0, got %d", c.Database.BoundedStorage.ActorContextLimit)
	}
	if c.Database.BoundedStorage.ActorContextsLimit < 0 {
		return errors.Newf("database.bounded_storage.actor_contexts_limit must be >= 0, got %d", c.Database.BoundedStorage.ActorContextsLimit)
	}
	if c.Database.BoundedStorage.EntityActorsLimit < 0 {
		return errors.Newf("database.bounded_storage.entity_actors_limit must be >= 0, got %d", c.Database.BoundedStorage.EntityActorsLimit)
	}

	return nil
}
