package am

import (
	"fmt"
	"sort"

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

	// Local Inference (Ollama/LocalAI) defaults
	v.SetDefault("local_inference.enabled", false) // Disabled by default - users should opt-in to local providers
	v.SetDefault("local_inference.base_url", "http://localhost:11434")
	v.SetDefault("local_inference.model", "llama3.2:3b")
	v.SetDefault("local_inference.context_size", 16384)
	v.SetDefault("local_inference.timeout_seconds", 360) // 6 minutes - reasonable for slow inference
	v.SetDefault("local_inference.onnx_model_path", "ats/vidstream/models/yolo11n.onnx")

	// OpenRouter defaults
	v.SetDefault("openrouter.model", "openai/gpt-4o-mini") // Cost-effective default
	v.SetDefault("openrouter.temperature", 0.2)            // Deterministic
	v.SetDefault("openrouter.max_tokens", 1000)            // Token limit

	// Ax (attestation query) defaults
	v.SetDefault("ax.default_actor", "ax@user")

	// Pulse (async job infrastructure) defaults
	v.SetDefault("pulse.workers", 1)
	v.SetDefault("pulse.ticker_interval_seconds", 1)
	v.SetDefault("pulse.daily_budget_usd", 3.0) // Default $3/day limit
	v.SetDefault("pulse.weekly_budget_usd", 7.0)               // Default $7/week limit
	v.SetDefault("pulse.monthly_budget_usd", 15.0)             // Default $15/month limit
	v.SetDefault("pulse.cost_per_score_usd", 0.002)            // Default $0.002 per operation

	// Server configuration defaults
	v.SetDefault("server.port", DefaultServerPort)
	v.SetDefault("server.frontend_port", 8820) // Frontend dev server port
	v.SetDefault("server.allowed_origins", []string{
		"http://localhost",
		"https://localhost",
		"http://127.0.0.1",
		"https://127.0.0.1",
		"tauri://localhost", // Allow Tauri desktop app
	})
	v.SetDefault("server.log_theme", "everforest")

	// Plugin configuration defaults
	v.SetDefault("plugin.enabled", []string{}) // No plugins enabled by default (explicit opt-in via am.toml)
	v.SetDefault("plugin.paths", []string{
		"~/.qntx/plugins", // User-level plugins
		"./plugins",       // Project-level plugins
	})
	v.SetDefault("plugin.websocket.keepalive.enabled", true)
	v.SetDefault("plugin.websocket.keepalive.ping_interval_secs", 30)
	v.SetDefault("plugin.websocket.keepalive.pong_timeout_secs", 60)
	v.SetDefault("plugin.websocket.keepalive.reconnect_attempts", 3)
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

// GetServerPort returns the configured QNTX server port
// Returns server.port from config, or DefaultServerPort (877) if not configured
func GetServerPort() int {
	cfg, err := Load()
	if err != nil || cfg.Server.Port == nil {
		return DefaultServerPort
	}
	return *cfg.Server.Port
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
// Merges configured origins with secure defaults, ensuring critical origins
// (localhost, 127.0.0.1, tauri) are always included even if not in config
func (c *Config) GetServerAllowedOrigins() []string {
	// Define secure default origins that should always be allowed
	defaults := []string{
		"http://localhost",
		"https://localhost",
		"http://127.0.0.1",
		"https://127.0.0.1",
		"tauri://localhost", // Allow Tauri desktop app
	}

	// If no custom origins configured, return defaults
	if len(c.Server.AllowedOrigins) == 0 {
		return defaults
	}

	// Merge: Start with defaults, add custom origins (deduplicated via map)
	originSet := make(map[string]bool)
	for _, origin := range defaults {
		originSet[origin] = true
	}
	for _, origin := range c.Server.AllowedOrigins {
		originSet[origin] = true
	}

	// Convert map to slice and sort for deterministic output
	merged := make([]string, 0, len(originSet))
	for origin := range originSet {
		merged = append(merged, origin)
	}
	sort.Strings(merged)

	return merged
}

// GetServerLogTheme returns the log theme (default: everforest)
func (c *Config) GetServerLogTheme() string {
	if c.Server.LogTheme == "" {
		return "everforest"
	}
	return c.Server.LogTheme
}

// String returns a string representation of the config
func (c *Config) String() string {
	return fmt.Sprintf("Config{Database: %s, Server: {LogTheme: %s}, Pulse: {Workers: %d}}",
		c.Database.Path, c.Server.LogTheme, c.Pulse.Workers)
}
