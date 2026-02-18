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
	Embeddings     EmbeddingsConfig     `mapstructure:"embeddings"`
	Sync           SyncConfig           `mapstructure:"sync"`
}

// SyncConfig configures peer-to-peer attestation sync
type SyncConfig struct {
	Name            string            `mapstructure:"name"`             // advertised to peers in hello (e.g., "laptop")
	IntervalSeconds int               `mapstructure:"interval_seconds"` // 0 = manual only
	Peers           map[string]string `mapstructure:"peers"`            // name = "url" (e.g., phone = "http://phone.local:877")
}

// DatabaseConfig configures the SQLite database
type DatabaseConfig struct {
	Path           string               `mapstructure:"path"`
	BoundedStorage BoundedStorageConfig `mapstructure:"bounded_storage"`
}

// BoundedStorageConfig configures storage limits for attestations.
// Values <= 0 default to: ActorContextLimit=16, ActorContextsLimit=64, EntityActorsLimit=64.
type BoundedStorageConfig struct {
	ActorContextLimit  int `mapstructure:"actor_context_limit"`  // attestations per (actor, context) pair (default: 16)
	ActorContextsLimit int `mapstructure:"actor_contexts_limit"` // contexts per actor (default: 64)
	EntityActorsLimit  int `mapstructure:"entity_actors_limit"`  // actors per entity (default: 64)
}

// ServerConfig configures the QNTX web server
type ServerConfig struct {
	Port           *int     `mapstructure:"port"`          // Server port: nil = default 877, 0 is invalid (omit for default)
	FrontendPort   int      `mapstructure:"frontend_port"` // Frontend dev server port (default: 8820)
	AllowedOrigins []string `mapstructure:"allowed_origins"`
	LogTheme       string   `mapstructure:"log_theme"` // Color theme: gruvbox, everforest
}

// Server port constants
const (
	DefaultServerPort     = 877  // Development port (easy to type, above privileged range)
	DefaultGraphEventPort = 878  // Event viewer port
	FallbackServerPort    = 7878 // Production fallback port
)

// PulseConfig configures the Pulse async job system (core infrastructure)
type PulseConfig struct {
	// Worker concurrency configuration
	Workers int `mapstructure:"workers"` // Number of concurrent job workers (default: 1)

	// Ticker configuration for scheduled job execution
	TickerIntervalSeconds int `mapstructure:"ticker_interval_seconds"` // How often to check for scheduled jobs (default: 1)

	// Node-level budget tracking (enforced locally per node)
	DailyBudgetUSD   float64 `mapstructure:"daily_budget_usd"`   // Daily spending limit in USD
	WeeklyBudgetUSD  float64 `mapstructure:"weekly_budget_usd"`  // Weekly spending limit in USD
	MonthlyBudgetUSD float64 `mapstructure:"monthly_budget_usd"` // Monthly spending limit in USD
	CostPerScoreUSD  float64 `mapstructure:"cost_per_score_usd"` // Estimated cost per operation

	// Cluster-level budget (enforced against aggregate spend across all nodes).
	// Effective limit = average of all nodes' configured cluster limits.
	// 0 = no cluster-level enforcement.
	ClusterDailyBudgetUSD   float64 `mapstructure:"cluster_daily_budget_usd"`
	ClusterWeeklyBudgetUSD  float64 `mapstructure:"cluster_weekly_budget_usd"`
	ClusterMonthlyBudgetUSD float64 `mapstructure:"cluster_monthly_budget_usd"`
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
	TimeoutSeconds int    `mapstructure:"timeout_seconds"` // Request timeout in seconds
	ContextSize    *int   `mapstructure:"context_size"`    // Context window size (nil = model default, e.g., 16384, 32768)
	ONNXModelPath  string `mapstructure:"onnx_model_path"` // Path to ONNX model for vidstream (default: ats/vidstream/models/yolo11n.onnx)
}

// OpenRouterConfig configures OpenRouter.ai API access
type OpenRouterConfig struct {
	APIKey      string   `mapstructure:"api_key"`     // OpenRouter API key
	Model       string   `mapstructure:"model"`       // Default model (e.g., "openai/gpt-4o-mini")
	Temperature *float64 `mapstructure:"temperature"` // Sampling temperature (nil = default 0.2)
	MaxTokens   *int     `mapstructure:"max_tokens"`  // Maximum tokens per request (nil = default 1000)
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
	PingIntervalSecs  *int `mapstructure:"ping_interval_secs"` // Seconds between PING messages (nil = default 30)
	PongTimeoutSecs   *int `mapstructure:"pong_timeout_secs"`  // Seconds to wait for PONG (nil = default 60)
	ReconnectAttempts *int `mapstructure:"reconnect_attempts"` // Number of reconnect attempts (nil = default 3)
}

// EmbeddingsConfig configures the embedding service for semantic search
type EmbeddingsConfig struct {
	Enabled                  bool     `mapstructure:"enabled"`                    // Enable embedding service (default: false)
	Path                     string   `mapstructure:"path"`                       // Path to ONNX model file
	Name                     string   `mapstructure:"name"`                       // Model identifier for metadata
	ClusterThreshold         float64  `mapstructure:"cluster_threshold"`          // Minimum similarity for cluster assignment (default: 0.5)
	ReclusterIntervalSeconds int      `mapstructure:"recluster_interval_seconds"` // Pulse schedule interval for HDBSCAN re-clustering (0 = disabled)
	ReprojectIntervalSeconds int      `mapstructure:"reproject_interval_seconds"` // Pulse schedule interval for UMAP re-projection (0 = disabled)
	MinClusterSize           int      `mapstructure:"min_cluster_size"`           // Minimum cluster size for HDBSCAN (default: 5)
	ClusterMatchThreshold    float64  `mapstructure:"cluster_match_threshold"`    // Cosine similarity threshold for stable cluster matching (default: 0.7)
	ProjectionMethods        []string `mapstructure:"projection_methods"`         // Dimensionality reduction methods: umap, tsne, pca (default: ["umap"])

	// Cluster labeling via LLM
	ClusterLabelIntervalSeconds int    `mapstructure:"cluster_label_interval_seconds"` // Pulse schedule interval (0 = disabled)
	ClusterLabelMinSize         int    `mapstructure:"cluster_label_min_size"`         // Min members to be eligible for labeling
	ClusterLabelSampleSize      int    `mapstructure:"cluster_label_sample_size"`      // Random samples sent to LLM
	ClusterLabelMaxPerCycle     int    `mapstructure:"cluster_label_max_per_cycle"`    // Max clusters labeled per run
	ClusterLabelCooldownDays    int    `mapstructure:"cluster_label_cooldown_days"`    // Min days between re-labels
	ClusterLabelMaxTokens       int    `mapstructure:"cluster_label_max_tokens"`       // LLM max_tokens
	ClusterLabelModel           string `mapstructure:"cluster_label_model"`            // Model override (empty = system default)
}

// File system constants
const (
	DefaultDirPermissions  = 0755 // Standard directory permissions (rwxr-xr-x)
	DefaultFilePermissions = 0644 // Standard file permissions (rw-r--r--)
	ExecutablePermissions  = 0755 // Executable file permissions (rwxr-xr-x)
)
