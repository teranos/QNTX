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

	return nil
}
