package am

import (
	"fmt"

	"github.com/spf13/viper"
)

// SetDefaults configures default values for all configuration options
func SetDefaults(v *viper.Viper) {
	// Database defaults
	v.SetDefault("database.path", "qntx.db")
	v.SetDefault("database.bounded_storage.actor_context_limit", 16)  // 16 attestations per (actor, context)
	v.SetDefault("database.bounded_storage.actor_contexts_limit", 64) // 64 contexts per actor
	v.SetDefault("database.bounded_storage.entity_actors_limit", 64)  // 64 actors per entity

	// Code defaults
	v.SetDefault("code.gopls.enabled", true)
	v.SetDefault("code.gopls.workspace_root", ".")

	// Local Inference (Ollama) defaults
	v.SetDefault("local_inference.enabled", true)
	v.SetDefault("local_inference.base_url", "http://localhost:11434")
	v.SetDefault("local_inference.model", "llama3.2:3b")
	v.SetDefault("local_inference.context_size", 16384)
	v.SetDefault("local_inference.timeout_seconds", 3600)

	// OpenRouter defaults
	v.SetDefault("openrouter.model", "openai/gpt-4o-mini") // Cost-effective default
	v.SetDefault("openrouter.temperature", 0.2)            // Deterministic
	v.SetDefault("openrouter.max_tokens", 1000)            // Token limit

	// Ax (attestation query) defaults
	v.SetDefault("ax.default_actor", "ax@user")

	// Pulse (async job infrastructure) defaults
	v.SetDefault("pulse.workers", 1)
	v.SetDefault("pulse.ticker_interval_seconds", 1)
	v.SetDefault("pulse.http_max_requests_per_minute", 10)     // Prevents bot detection (LinkedIn HTTP 999)
	v.SetDefault("pulse.http_delay_between_requests_ms", 2000) // 2 second polite delay
	v.SetDefault("pulse.daily_budget_usd", 3.0)                // Default $3/day limit
	v.SetDefault("pulse.weekly_budget_usd", 7.0)               // Default $7/week limit
	v.SetDefault("pulse.monthly_budget_usd", 15.0)             // Default $15/month limit
	v.SetDefault("pulse.cost_per_score_usd", 0.002)            // Default $0.002 per operation

	// REPL configuration defaults
	v.SetDefault("repl.search.debounce_ms", 50)        // Search debounce delay
	v.SetDefault("repl.search.result_limit", 10)       // Max search results to show
	v.SetDefault("repl.search.exact_match_score", 100) // Score for exact matches
	v.SetDefault("repl.search.prefix_match_score", 50) // Score for prefix matches
	v.SetDefault("repl.search.contains_score", 25)     // Score for substring matches
	v.SetDefault("repl.search.base_result_score", 50)  // Base score for search results
	v.SetDefault("repl.search.length_bonus_score", 50) // Maximum length bonus

	v.SetDefault("repl.display.max_lines", 10)      // Max lines in preview mode
	v.SetDefault("repl.display.buffer_limit", 2000) // Max chars per result buffer
	v.SetDefault("repl.display.target_fps", 60)     // Target FPS for rendering

	v.SetDefault("repl.timeouts.command_seconds", 30)  // Command execution timeout
	v.SetDefault("repl.timeouts.database_seconds", 10) // Database query timeout

	v.SetDefault("repl.history.result_limit", 100)   // Max results in history
	v.SetDefault("repl.history.channel_buffer", 100) // Channel buffer size

	// Server configuration defaults
	v.SetDefault("server.port", DefaultGraphPort)
	v.SetDefault("server.allowed_origins", []string{
		"http://localhost",
		"https://localhost",
		"http://127.0.0.1",
		"https://127.0.0.1",
		"tauri://localhost", // Allow Tauri desktop app
	})
	v.SetDefault("server.log_theme", "everforest")
}

// BindSensitiveEnvVars explicitly binds sensitive configuration to environment variables
func BindSensitiveEnvVars(v *viper.Viper) {
	// Code command configuration
	v.BindEnv("code.github.token", "QNTX_CODE_GITHUB_TOKEN")

	// Database path
	v.BindEnv("database.path", "QNTX_DATABASE_PATH")

	// Local inference configuration
	v.BindEnv("local_inference.enabled", "QNTX_LOCAL_INFERENCE_ENABLED")
	v.BindEnv("local_inference.base_url", "QNTX_LOCAL_INFERENCE_BASE_URL")
	v.BindEnv("local_inference.model", "QNTX_LOCAL_INFERENCE_MODEL")
}

// GetGraphPort returns the configured QNTX server port
// Returns server.port from config, or DefaultGraphPort (877) if not configured
func GetGraphPort() int {
	cfg, err := Load()
	if err != nil || cfg.Server.Port == 0 {
		return DefaultGraphPort
	}
	return cfg.Server.Port
}

// GetGraphEventPort returns the event viewer port
func GetGraphEventPort() int {
	return DefaultGraphEventPort
}

// GetDatabasePath returns the configured database path
func (c *Config) GetDatabasePath() string {
	if c.Database.Path == "" {
		return "qntx.db" // Fallback default
	}
	return c.Database.Path
}

// GetServerAllowedOrigins returns the allowed CORS origins
func (c *Config) GetServerAllowedOrigins() []string {
	if len(c.Server.AllowedOrigins) == 0 {
		return []string{
			"http://localhost",
			"https://localhost",
			"http://127.0.0.1",
			"https://127.0.0.1",
			"tauri://localhost", // Allow Tauri desktop app
		}
	}
	return c.Server.AllowedOrigins
}

// GetServerLogTheme returns the log theme (default: everforest)
func (c *Config) GetServerLogTheme() string {
	if c.Server.LogTheme == "" {
		return "everforest"
	}
	return c.Server.LogTheme
}

// GetREPLConfig returns the REPL configuration with defaults applied
func (c *Config) GetREPLConfig() REPLConfig {
	cfg := c.REPL

	// Apply defaults for zero values
	if cfg.Search.DebounceMs == 0 {
		cfg.Search.DebounceMs = 50
	}
	if cfg.Search.ResultLimit == 0 {
		cfg.Search.ResultLimit = 10
	}
	if cfg.Search.ExactMatchScore == 0 {
		cfg.Search.ExactMatchScore = 100
	}
	if cfg.Search.PrefixMatchScore == 0 {
		cfg.Search.PrefixMatchScore = 50
	}
	if cfg.Search.ContainsScore == 0 {
		cfg.Search.ContainsScore = 25
	}
	if cfg.Search.BaseResultScore == 0 {
		cfg.Search.BaseResultScore = 50
	}
	if cfg.Search.LengthBonusScore == 0 {
		cfg.Search.LengthBonusScore = 50
	}

	if cfg.Display.MaxLines == 0 {
		cfg.Display.MaxLines = 10
	}
	if cfg.Display.BufferLimit == 0 {
		cfg.Display.BufferLimit = 2000
	}
	if cfg.Display.TargetFPS == 0 {
		cfg.Display.TargetFPS = 60
	}

	if cfg.Timeouts.CommandSeconds == 0 {
		cfg.Timeouts.CommandSeconds = 30
	}
	if cfg.Timeouts.DatabaseSeconds == 0 {
		cfg.Timeouts.DatabaseSeconds = 10
	}

	if cfg.History.ResultLimit == 0 {
		cfg.History.ResultLimit = 100
	}
	if cfg.History.ChannelBuffer == 0 {
		cfg.History.ChannelBuffer = 100
	}

	return cfg
}

// String returns a string representation of the config
func (c *Config) String() string {
	return fmt.Sprintf("Config{Database: %s, Server: {LogTheme: %s}, Pulse: {Workers: %d}}",
		c.Database.Path, c.Server.LogTheme, c.Pulse.Workers)
}



