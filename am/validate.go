package am

import "fmt"

// Validate checks that the configuration is valid
func (c *Config) Validate() error {
	// Database path is optional - empty defaults to "qntx.db" per defaults.go:11
	// No validation needed here

	// Pulse workers: 0 = disabled, negative = invalid
	if c.Pulse.Workers < 0 {
		return fmt.Errorf("pulse.workers must be >= 0 (0 = disabled), got %d", c.Pulse.Workers)
	}

	// Pulse ticker interval: 0 = disabled, negative = invalid
	if c.Pulse.TickerIntervalSeconds < 0 {
		return fmt.Errorf("pulse.ticker_interval_seconds must be >= 0 (0 = disabled), got %d", c.Pulse.TickerIntervalSeconds)
	}

	// HTTP rate limiting: 0 = unlimited, negative = invalid
	if c.Pulse.HTTPMaxRequestsPerMinute < 0 {
		return fmt.Errorf("pulse.http_max_requests_per_minute must be >= 0 (0 = unlimited), got %d", c.Pulse.HTTPMaxRequestsPerMinute)
	}
	if c.Pulse.HTTPDelayBetweenRequestsMS < 0 {
		return fmt.Errorf("pulse.http_delay_between_requests_ms must be >= 0, got %d", c.Pulse.HTTPDelayBetweenRequestsMS)
	}

	// Validate local inference configuration only when enabled
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

	return nil
}
