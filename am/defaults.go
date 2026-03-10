package am

import (
	"fmt"
	"os"
	"path/filepath"
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
	// context_size is optional: nil = model default (checked in ai/provider/local_provider.go)
	v.SetDefault("local_inference.timeout_seconds", 360) // 6 minutes - reasonable for slow inference
	v.SetDefault("local_inference.onnx_model_path", "ats/vidstream/models/yolo11n.onnx")

	// OpenRouter configuration is now handled by the qntx-openrouter plugin.
	// Plugin config keys are read via plugin.Config.GetString("api_key") etc.

	// Ax (attestation query) defaults
	v.SetDefault("ax.default_actor", "ax@user")

	// Pulse (async job infrastructure) defaults
	v.SetDefault("pulse.workers", 1)
	v.SetDefault("pulse.ticker_interval_seconds", 1)
	v.SetDefault("pulse.daily_budget_usd", 3.0)     // Default $3/day limit
	v.SetDefault("pulse.weekly_budget_usd", 7.0)    // Default $7/week limit
	v.SetDefault("pulse.monthly_budget_usd", 15.0)  // Default $15/month limit
	v.SetDefault("pulse.cost_per_score_usd", 0.002) // Default $0.002 per operation

	// Auth defaults (disabled by default — zero auth code runs when disabled)
	v.SetDefault("auth.enabled", false)
	v.SetDefault("auth.session_expiry_hours", 24)

	// Server configuration defaults
	v.SetDefault("server.port", DefaultServerPort)
	v.SetDefault("server.bind_address", "127.0.0.1") // Loopback only — safe default, no auth required
	v.SetDefault("server.frontend_port", 8820)       // Frontend dev server port
	v.SetDefault("server.allowed_origins", []string{
		"http://localhost",
		"https://localhost",
		"http://127.0.0.1",
		"https://127.0.0.1",
		"tauri://localhost", // Allow Tauri desktop app
	})
	v.SetDefault("server.log_theme", "everforest")

	// Rate limiting defaults (per-IP token bucket)
	v.SetDefault("server.rate_limit.auth_rate", 2.0)
	v.SetDefault("server.rate_limit.auth_burst", 5)
	v.SetDefault("server.rate_limit.ws_rate", 2.0)
	v.SetDefault("server.rate_limit.ws_burst", 10)
	v.SetDefault("server.rate_limit.write_rate", 20.0)
	v.SetDefault("server.rate_limit.write_burst", 40)
	v.SetDefault("server.rate_limit.read_rate", 60.0)
	v.SetDefault("server.rate_limit.read_burst", 120)
	v.SetDefault("server.rate_limit.public_rate", 10.0)
	v.SetDefault("server.rate_limit.public_burst", 20)

	// Embeddings (semantic search) defaults
	v.SetDefault("embeddings.enabled", false) // Disabled by default - requires ONNX model
	v.SetDefault("embeddings.path", "ats/embeddings/models/all-MiniLM-L6-v2/model.onnx")
	v.SetDefault("embeddings.name", "all-MiniLM-L6-v2")
	v.SetDefault("embeddings.cluster_threshold", 0.5) // Minimum cosine similarity for cluster prediction
	// recluster_interval_seconds: omit for default (not scheduled). Set positive value to enable.
	// reproject_interval_seconds: omit for default (not scheduled). Set positive value to enable.
	v.SetDefault("embeddings.min_cluster_size", 5)                  // Minimum cluster size for HDBSCAN
	v.SetDefault("embeddings.cluster_match_threshold", 0.7)         // Cosine similarity for stable cluster matching
	v.SetDefault("embeddings.projection_methods", []string{"umap"}) // Dimensionality reduction methods

	// Cluster labeling via LLM
	// cluster_label_interval_seconds: omit for default (not scheduled). Set positive value to enable.
	v.SetDefault("embeddings.cluster_label_min_size", 15)
	v.SetDefault("embeddings.cluster_label_sample_size", 5)
	v.SetDefault("embeddings.cluster_label_max_per_cycle", 3)
	v.SetDefault("embeddings.cluster_label_cooldown_days", 7)
	v.SetDefault("embeddings.cluster_label_max_tokens", 2000)
	v.SetDefault("embeddings.cluster_label_model", "") // empty = system default

	// Watcher defaults
	v.SetDefault("watcher.max_fires_per_second", 3)

	// Plugin configuration defaults
	v.SetDefault("plugin.enabled", []string{}) // No plugins enabled by default (explicit opt-in via am.toml)
	v.SetDefault("plugin.paths", []string{
		"~/.qntx/plugins",
	})
	v.SetDefault("plugin.websocket.keepalive.enabled", true)
	// ping_interval_secs, pong_timeout_secs, reconnect_attempts are optional: nil = defaults (30, 60, 3) in plugin/grpc/websocket_keepalive.go

	// Runtime defaults - auto-detect QNTX root or use env var
	if tsRuntime := findTypeScriptRuntime(); tsRuntime != "" {
		v.SetDefault("plugin.runtime.typescript_runtime", tsRuntime)
	}
}

// findTypeScriptRuntime locates the TypeScript runtime (main.ts)
// Checks QNTX_ROOT env var, then walks up from CWD looking for go.mod
func findTypeScriptRuntime() string {
	// 1. Check env var QNTX_ROOT
	if root := os.Getenv("QNTX_ROOT"); root != "" {
		runtimePath := filepath.Join(root, "plugin/typescript/runtime/main.ts")
		if _, err := os.Stat(runtimePath); err == nil {
			return runtimePath
		}
	}

	// 2. Walk up from CWD looking for go.mod (QNTX root marker)
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}

	for {
		// Check if this directory contains go.mod
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			// Found QNTX root - check if runtime exists
			runtimePath := filepath.Join(dir, "plugin/typescript/runtime/main.ts")
			if _, err := os.Stat(runtimePath); err == nil {
				return runtimePath
			}
		}

		// Move up one directory
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			break
		}
		dir = parent
	}

	return ""
}

// BindSensitiveEnvVars explicitly binds sensitive configuration to environment variables
func BindSensitiveEnvVars(v *viper.Viper) {
	// Code command configuration
	v.BindEnv("code.github.token", "QNTX_CODE_GITHUB_TOKEN")

	// Database path
	v.BindEnv("database.path", "QNTX_DATABASE_PATH")

	// Server bind address (e.g., "0.0.0.0" for all interfaces — requires auth.enabled)
	v.BindEnv("server.bind_address", "QNTX_BIND_ADDRESS")

	// Local inference configuration
	v.BindEnv("local_inference.enabled", "QNTX_LOCAL_INFERENCE_ENABLED")
	v.BindEnv("local_inference.base_url", "QNTX_LOCAL_INFERENCE_BASE_URL")
	v.BindEnv("local_inference.model", "QNTX_LOCAL_INFERENCE_MODEL")
}

// IsLoopbackAddress returns true if the address is a loopback address (127.0.0.1, ::1, localhost)
func IsLoopbackAddress(addr string) bool {
	return addr == "" || addr == "127.0.0.1" || addr == "::1" || addr == "localhost"
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
		"http://localhost:*",
		"https://localhost",
		"https://localhost:*",
		"http://127.0.0.1",
		"http://127.0.0.1:*",
		"https://127.0.0.1",
		"https://127.0.0.1:*",
		"tauri://localhost",
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

// GetLogPath returns the file log path. If not configured, defaults to tmp/qntx-{port}.log.
func (c *Config) GetLogPath(port int) string {
	if c.Server.LogPath != "" {
		return c.Server.LogPath
	}
	return fmt.Sprintf("tmp/qntx-%d.log", port)
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
