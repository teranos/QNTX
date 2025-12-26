package am

import "fmt"

// Validate checks that the configuration is valid
func (c *Config) Validate() error {
	// Validate database path
	if c.Database.Path == "" {
		return fmt.Errorf("database.path cannot be empty")
	}

	// Validate pulse workers
	if c.Pulse.Workers < 0 {
		return fmt.Errorf("pulse.workers must be >= 0, got %d", c.Pulse.Workers)
	}

	// Validate pulse ticker interval
	if c.Pulse.TickerIntervalSeconds < 0 {
		return fmt.Errorf("pulse.ticker_interval_seconds must be >= 0, got %d", c.Pulse.TickerIntervalSeconds)
	}

	// Validate HTTP rate limiting
	if c.Pulse.HTTPMaxRequestsPerMinute < 0 {
		return fmt.Errorf("pulse.http_max_requests_per_minute must be >= 0, got %d", c.Pulse.HTTPMaxRequestsPerMinute)
	}
	if c.Pulse.HTTPDelayBetweenRequestsMS < 0 {
		return fmt.Errorf("pulse.http_delay_between_requests_ms must be >= 0, got %d", c.Pulse.HTTPDelayBetweenRequestsMS)
	}

	// Validate local inference configuration
	if c.LocalInference.Enabled {
		if c.LocalInference.BaseURL == "" {
			return fmt.Errorf("local_inference.base_url cannot be empty when enabled")
		}
		if c.LocalInference.Model == "" {
			return fmt.Errorf("local_inference.model cannot be empty when enabled")
		}
		if c.LocalInference.TimeoutSeconds <= 0 {
			return fmt.Errorf("local_inference.timeout_seconds must be > 0, got %d", c.LocalInference.TimeoutSeconds)
		}
	}

	// Validate REPL timeouts
	if c.REPL.Timeouts.CommandSeconds <= 0 {
		return fmt.Errorf("repl.timeouts.command_seconds must be > 0, got %d", c.REPL.Timeouts.CommandSeconds)
	}
	if c.REPL.Timeouts.DatabaseSeconds <= 0 {
		return fmt.Errorf("repl.timeouts.database_seconds must be > 0, got %d", c.REPL.Timeouts.DatabaseSeconds)
	}

	return nil
}
