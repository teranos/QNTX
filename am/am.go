package am

// Config represents the core QNTX configuration
type Config struct {
	Database       DatabaseConfig       `mapstructure:"database"`
	Server         ServerConfig         `mapstructure:"server"`
	Pulse          PulseConfig          `mapstructure:"pulse"`
	Code           CodeConfig           `mapstructure:"code"`
	LocalInference LocalInferenceConfig `mapstructure:"local_inference"`
	OpenRouter     OpenRouterConfig     `mapstructure:"openrouter"`
	Ax             AxConfig             `mapstructure:"ax"`
	Plugin         PluginConfig         `mapstructure:"plugin"`
	Auth           AuthConfig           `mapstructure:"auth"`
}

// AuthConfig configures authentication for remote clients
type AuthConfig struct {
	Enabled       bool               `mapstructure:"enabled"`        // Enable authentication (default: false for local-only)
	JWTSecret     string             `mapstructure:"jwt_secret"`     // Secret for signing JWTs (auto-generated if empty)
	TokenExpiry   string             `mapstructure:"token_expiry"`   // JWT token expiry duration (default: 15m)
	RefreshExpiry string             `mapstructure:"refresh_expiry"` // Refresh token expiry (default: 30d)
	GitHub        AuthGitHubConfig   `mapstructure:"github"`         // GitHub OAuth provider
	TLS           AuthTLSConfig      `mapstructure:"tls"`            // TLS/HTTPS configuration
}

// AuthGitHubConfig configures GitHub OAuth provider
type AuthGitHubConfig struct {
	Enabled      bool   `mapstructure:"enabled"`       // Enable GitHub OAuth
	ClientID     string `mapstructure:"client_id"`     // GitHub OAuth App Client ID
	ClientSecret string `mapstructure:"client_secret"` // GitHub OAuth App Client Secret (use env var)
}

// AuthTLSConfig configures TLS/HTTPS for secure connections
type AuthTLSConfig struct {
	Enabled  bool   `mapstructure:"enabled"`   // Enable HTTPS
	CertFile string `mapstructure:"cert_file"` // Path to TLS certificate file
	KeyFile  string `mapstructure:"key_file"`  // Path to TLS private key file
}

// DatabaseConfig configures the SQLite database
type DatabaseConfig struct {
	Path           string               `mapstructure:"path"`
	BoundedStorage BoundedStorageConfig `mapstructure:"bounded_storage"`
}

// BoundedStorageConfig configures storage limits for attestations
type BoundedStorageConfig struct {
	ActorContextLimit  int `mapstructure:"actor_context_limit"`  // attestations per (actor, context) pair
	ActorContextsLimit int `mapstructure:"actor_contexts_limit"` // contexts per actor
	EntityActorsLimit  int `mapstructure:"entity_actors_limit"`  // actors per entity
}

// ServerConfig configures the QNTX web server
type ServerConfig struct {
	Port           int      `mapstructure:"port"` // Server port (default: 877)
	AllowedOrigins []string `mapstructure:"allowed_origins"`
	LogTheme       string   `mapstructure:"log_theme"` // Color theme: gruvbox, everforest
}

// Server port constants
const (
	DefaultGraphPort      = 877  // Development port (easy to type, above privileged range)
	DefaultGraphEventPort = 878  // Event viewer port
	FallbackGraphPort     = 7878 // Production fallback port
)

// PulseConfig configures the Pulse async job system (core infrastructure)
type PulseConfig struct {
	// Worker concurrency configuration
	Workers int `mapstructure:"workers"` // Number of concurrent job workers (default: 1)

	// Ticker configuration for scheduled job execution
	TickerIntervalSeconds int `mapstructure:"ticker_interval_seconds"` // How often to check for scheduled jobs (default: 1)

	// HTTP rate limiting (prevents bot detection like LinkedIn HTTP 999)
	// Default settings for sites without specific config
	HTTPMaxRequestsPerMinute   int `mapstructure:"http_max_requests_per_minute"`
	HTTPDelayBetweenRequestsMS int `mapstructure:"http_delay_between_requests_ms"`

	// Per-domain rate limit overrides (key = domain like "linkedin.com")
	HTTPDomainLimits map[string]HTTPDomainLimit `mapstructure:"http_domain_limits"`

	// Budget tracking and rate limiting (core Pulse features)
	DailyBudgetUSD        float64 `mapstructure:"daily_budget_usd"`         // Daily spending limit in USD
	WeeklyBudgetUSD       float64 `mapstructure:"weekly_budget_usd"`        // Weekly spending limit in USD
	MonthlyBudgetUSD      float64 `mapstructure:"monthly_budget_usd"`       // Monthly spending limit in USD
	CostPerScoreUSD       float64 `mapstructure:"cost_per_score_usd"`       // Estimated cost per operation
	MaxCallsPerMinute     int     `mapstructure:"max_calls_per_minute"`     // Rate limit for API calls
	PauseOnBudgetExceeded bool    `mapstructure:"pause_on_budget_exceeded"` // Pause jobs when budget exceeded (vs fail them)
}

// HTTPDomainLimit configures per-domain HTTP rate limiting
type HTTPDomainLimit struct {
	MaxRequestsPerMinute   int `mapstructure:"max_requests_per_minute"`
	DelayBetweenRequestsMS int `mapstructure:"delay_between_requests_ms"`
}

// CodeConfig configures the code review system
type CodeConfig struct {
	GitHub CodeGitHubConfig `mapstructure:"github"`
	Gopls  CodeGoplsConfig  `mapstructure:"gopls"`
}

// CodeGitHubConfig configures GitHub integration for code review
type CodeGitHubConfig struct {
	Token string `mapstructure:"token"`
}

// CodeGoplsConfig configures gopls (Go language server) integration
type CodeGoplsConfig struct {
	WorkspaceRoot string `mapstructure:"workspace_root"` // Workspace root for gopls (default: project root)
	Enabled       bool   `mapstructure:"enabled"`        // Enable gopls integration (default: true)
}

// LocalInferenceConfig configures local model inference (Ollama, LocalAI, etc.)
type LocalInferenceConfig struct {
	Enabled        bool   `mapstructure:"enabled"`         // Enable local inference instead of cloud APIs
	BaseURL        string `mapstructure:"base_url"`        // e.g., "http://localhost:11434" for Ollama
	Model          string `mapstructure:"model"`           // e.g., "mistral", "qwen2.5-coder:7b"
	TimeoutSeconds int    `mapstructure:"timeout_seconds"` // Request timeout (default: 120)
	ContextSize    int    `mapstructure:"context_size"`    // Context window size (0 = model default, e.g., 16384, 32768)
}

// OpenRouterConfig configures OpenRouter.ai API access
type OpenRouterConfig struct {
	APIKey      string  `mapstructure:"api_key"`     // OpenRouter API key
	Model       string  `mapstructure:"model"`       // Default model (e.g., "openai/gpt-4o-mini")
	Temperature float64 `mapstructure:"temperature"` // Sampling temperature (default: 0.2)
	MaxTokens   int     `mapstructure:"max_tokens"`  // Maximum tokens per request (default: 1000)
}

// AxConfig configures the attestation query system
type AxConfig struct {
	DefaultActor string `mapstructure:"default_actor"`
}

// PluginConfig configures the domain plugin system
type PluginConfig struct {
	Enabled   []string              `mapstructure:"enabled"`   // Whitelist of enabled plugins (e.g., ["code"])
	Paths     []string              `mapstructure:"paths"`     // Plugin search paths (e.g., ["~/.qntx/plugins", "./plugins"])
	WebSocket PluginWebSocketConfig `mapstructure:"websocket"` // WebSocket configuration
}

// PluginWebSocketConfig configures WebSocket keepalive behavior
type PluginWebSocketConfig struct {
	Keepalive PluginKeepaliveConfig `mapstructure:"keepalive"`
}

// PluginKeepaliveConfig configures WebSocket keepalive behavior
type PluginKeepaliveConfig struct {
	Enabled           bool `mapstructure:"enabled"`            // Enable keepalive (default: true)
	PingIntervalSecs  int  `mapstructure:"ping_interval_secs"` // Seconds between PING messages (default: 30)
	PongTimeoutSecs   int  `mapstructure:"pong_timeout_secs"`  // Seconds to wait for PONG (default: 60)
	ReconnectAttempts int  `mapstructure:"reconnect_attempts"` // Number of reconnect attempts (default: 3)
}

// File system constants
const (
	DefaultDirPermissions  = 0755 // Standard directory permissions (rwxr-xr-x)
	DefaultFilePermissions = 0644 // Standard file permissions (rw-r--r--)
	ExecutablePermissions  = 0755 // Executable file permissions (rwxr-xr-x)
)
