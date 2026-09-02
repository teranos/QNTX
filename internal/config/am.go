package config

// Config represents the core QNTX configuration
type Config struct {
	Storage      StorageConfig    `mapstructure:"storage"`
	Server       ServerConfig     `mapstructure:"server"`
	Auth         AuthConfig       `mapstructure:"auth"`
	Pulse        PulseConfig      `mapstructure:"pulse"`
	LLM          LLMConfig        `mapstructure:"llm"`
	Code         CodeConfig       `mapstructure:"code"`
	Ax           AxConfig         `mapstructure:"ax"`
	Plugin       PluginConfig     `mapstructure:"plugin"`
	Embeddings   EmbeddingsConfig `mapstructure:"embeddings"`
	Watcher      WatcherConfig    `mapstructure:"watcher"`
	Fetch        FetchConfig      `mapstructure:"fetch"`
	Distill      DistillConfig    `mapstructure:"distill"`
	Sentry       SentryConfig     `mapstructure:"sentry"`
	GroundDBPath string           `mapstructure:"ground_db_path"` // Path to Ground's database for deferred news delivery
}

// SentryConfig points the node's logs at a Sentry project.
//
// The console and the log file are for the box. This is for when nobody is on
// the box: it ships what the global logger already writes, so nothing about
// where a log is written changes when it is turned on.
//
// An empty DSN is off. Every other field here is read only when one is set.
type SentryConfig struct {
	DSN           string   `mapstructure:"dsn"`            // Where logs go. Empty = nothing is shipped. A DSN is an ingest key, not a secret: it can only write, and it is meant to travel with the build.
	Environment   string   `mapstructure:"environment"`    // Which node this is, in Sentry's own vocabulary (e.g. "production", "laptop"). Empty = Sentry reads SENTRY_ENVIRONMENT, then nothing.
	ServerName    string   `mapstructure:"server_name"`    // Names the node inside the environment. Empty = the hostname.
	MinLevel      string   `mapstructure:"min_level"`      // Lowest level that leaves the process: debug, info, warn, error. Below it stays local.
	CaptureErrors bool     `mapstructure:"capture_errors"` // Raise an issue, and not only a log line, at error and above. An issue is grouped across occurrences and carries the error's stack.
	FlushSeconds  int      `mapstructure:"flush_seconds"`  // How long the process waits at exit for the batch to drain.
	Debug         bool     `mapstructure:"debug"`          // Print what the SDK is doing to stderr. Answers "did it leave" without guessing.
}

// DistillConfig configures age-based attestation distillation.
// When enabled, a Pulse job periodically folds old attestations into
// compressed summaries, keeping the database from growing unbounded.
type DistillConfig struct {
	IntervalSeconds *int `mapstructure:"interval_seconds"` // nil = disabled, 600 = 10m dev, 21600 = 6h prod
	MaxAgeHours     int  `mapstructure:"max_age_hours"`    // Attestations older than this get distilled (default: 96 = 4d)
	BatchSize       int  `mapstructure:"batch_size"`       // Max attestations to process per tick (default: 500)
	DryRun          bool `mapstructure:"dry_run"`          // Log what would be distilled without doing it
}

// LLMConfig configures LLM request queuing and rate limiting at the core routing layer.
type LLMConfig struct {
	MaxConcurrent     int `mapstructure:"max_concurrent"`       // Max simultaneous provider calls (default: 1)
	MaxCallsPerMinute int `mapstructure:"max_calls_per_minute"` // Rate limit across all callers (default: 60)
	MaxQueueDepth     int `mapstructure:"max_queue_depth"`      // Max waiting requests before rejection (default: 25)
	CooldownSeconds   int `mapstructure:"cooldown_seconds"`     // Pause between inference runs (default: 3)
}

// FetchConfig configures the FetchService gRPC server (HTTP GET on behalf of plugins).
type FetchConfig struct {
	MaxRequestsPerWindow int `mapstructure:"max_requests_per_window"` // Max requests per window (default: 100)
	WindowSeconds        int `mapstructure:"window_seconds"`          // Rolling window duration in seconds (default: 300 = 5 min)
	PulseIntervalSeconds int `mapstructure:"pulse_interval_seconds"`  // Stats logging interval in seconds (default: 30)
}

// WatcherConfig configures the watcher engine
type WatcherConfig struct {
	MaxFiresPerSecond int `mapstructure:"max_fires_per_second"` // Default rate limit for new watchers (default: 3)
}

// AuthConfig configures biometric authentication (WebAuthn)
type AuthConfig struct {
	Enabled            bool                  `mapstructure:"enabled"`              // Enable biometric auth gate (default: false)
	SessionExpiryHours int                   `mapstructure:"session_expiry_hours"` // Session lifetime in hours (default: 24)
	RPID               string                `mapstructure:"rp_id"`                // WebAuthn Relying Party ID — the domain (e.g. "qntx.example.com"). Empty = "localhost" fallback for dev. Required when server.bind_address is non-loopback and auth.enabled is true.
	RPOrigins          []string              `mapstructure:"rp_origins"`           // WebAuthn Relying Party origins — full URLs (e.g. ["https://qntx.example.com"]). Empty = loopback URLs derived from server.port / server.frontend_port.
	RootIdentities     []string              `mapstructure:"root_identities"`      // Identities with full access. Either a did:key (a public key — the signature proves possession) or a provider account URL, which requires a binding signed by one of binding_signers. Empty = no identity may log in this way. Required when server.bind_address is non-loopback and auth.enabled is true.
	BindingSigners     []string              `mapstructure:"binding_signers"`      // Hex ed25519 public keys whose signature on an account binding is trusted. A binding carries its own signer, so without this list any peer can claim any account.
	PublicOrigin       string                `mapstructure:"public_origin"`        // The origin this node answers on (e.g. "https://api.example.com"), used to build the provider ceremony's redirect_uri. This is the API origin, not rp_origins, which is where the page is. Empty = read off the request, which trusts X-Forwarded-Host.
	Provider           ProviderConfig        `mapstructure:"provider"`             // Per-provider credentials the operator holds. A provider absent here is one that needs nothing: Mastodon registers its own app mid-ceremony, atproto spends a password the person types.
	Door               map[string]DoorConfig `mapstructure:"door"`                 // Front doors, keyed by the namespace behind each. A namespace absent here has no door, which is every namespace today. rp_id and rp_origins above are the door onto "default" and are not repeated here.
}

// DoorConfig is one front door: a domain people arrive at, and the namespace
// they arrive in.
//
// A passkey belongs to the domain it was made at, so a door is a relying party
// of its own. The rp id must be a registrable domain suffix of every origin
// under it — the browser's rule, which is why one rp id can stand behind
// several hostnames and a door with several origins is still one door.
type DoorConfig struct {
	RPID    string   `mapstructure:"rp_id"`   // WebAuthn Relying Party ID for this door — the domain (e.g. "garden.test").
	Origins []string `mapstructure:"origins"` // Full URLs a browser reaches this door at (e.g. ["https://portal.garden.test"]). Where the page is, never where the API answers.
	// Provider is this door's own OAuth clients, same shape as the node's.
	// One client for the whole node means somebody arriving at a door sees a
	// consent screen named after the node rather than after the thing they came
	// to. A door that names none falls back to the node's.
	Provider ProviderConfig `mapstructure:"provider"`
}

// ProviderConfig holds what an identity provider cannot supply for itself.
//
// Google is the first: its OAuth client is registered by the operator, in the
// operator's Google account, and a node without one has no Google to offer.
type ProviderConfig struct {
	Google OAuthClientConfig `mapstructure:"google"` // Registered at console.cloud.google.com
}

// OAuthClientConfig is one OAuth client an operator registered. Every such
// console asks for the same two things and hands back the same two things, so
// one type says it once.
//
// ClientSecret names the secret rather than being it — am.toml ships as a
// world-readable SSM String parameter, so a literal here is already disclosed.
// See internal/secretref.
type OAuthClientConfig struct {
	ClientID        string `mapstructure:"client_id"`     // Public half of the client, as the provider's console issues it
	ClientSecretRef string `mapstructure:"client_secret"` // ssm:// or env: reference — a literal is rejected
}

// StorageConfig selects the storage backend and holds backend-specific config.
// See ADR-023 for the selection model.
type StorageConfig struct {
	Backend string        `mapstructure:"backend"` // "sqlite" (default) or "parquet". Additional backends in subsequent ADRs.
	Sqlite  SqliteConfig  `mapstructure:"sqlite"`
	Parquet ParquetConfig `mapstructure:"parquet"`
}

// SqliteConfig configures the SQLite backend.
type SqliteConfig struct {
	Path                  string               `mapstructure:"path"`
	BackupIntervalSeconds int                  `mapstructure:"backup_interval_seconds"` // 0 = disabled, default 3600 (hourly)
	BoundedStorage        BoundedStorageConfig `mapstructure:"bounded_storage"`
}

// ParquetConfig configures the Parquet backend (see ADR-024).
// Location is a URL: "s3://bucket/prefix" (AWS Lightsail + S3 target)
// or "file:///path/to/dir" (development). No credentials field — AWS SDK's
// default chain resolves them.
type ParquetConfig struct {
	Location string `mapstructure:"location"`
}

// BoundedStorageConfig configures storage limits for attestations.
// Omit fields for defaults: ActorContextLimit=32, ActorContextsLimit=64, EntityActorsLimit=64.
type BoundedStorageConfig struct {
	ActorContextLimit  int `mapstructure:"actor_context_limit"`  // attestations per (actor, context) pair (default: 32)
	ActorContextsLimit int `mapstructure:"actor_contexts_limit"` // contexts per actor (default: 64)
	EntityActorsLimit  int `mapstructure:"entity_actors_limit"`  // actors per entity (default: 64)
}

// ServerConfig configures the QNTX web server
type ServerConfig struct {
	Port           *int            `mapstructure:"port"`          // Server port: nil = default 8770, 0 is invalid (omit for default)
	BindAddress    string          `mapstructure:"bind_address"`  // Network interface to bind: "127.0.0.1" (default, local only) or "0.0.0.0" (all interfaces)
	FrontendPort   int             `mapstructure:"frontend_port"` // Frontend dev server port (default: 8820)
	AllowedOrigins []string        `mapstructure:"allowed_origins"`
	LogPath        string          `mapstructure:"log_path"`   // File log path when verbosity >= 2 (default: tmp/qntx.log)
	LogTheme       string          `mapstructure:"log_theme"`  // Color theme: gruvbox, everforest
	RateLimit      RateLimitConfig `mapstructure:"rate_limit"` // Per-IP rate limiting
	PprofPort      int             `mapstructure:"pprof_port"` // Port for /debug/pprof, always bound to 127.0.0.1 regardless of bind_address (default: 8771)
}

// RateLimitConfig configures per-IP token bucket rate limits.
type RateLimitConfig struct {
	AuthRate    float64 `mapstructure:"auth_rate"`    // /auth/* requests per second (default: 2)
	AuthBurst   int     `mapstructure:"auth_burst"`   // /auth/* burst capacity (default: 5)
	WSRate      float64 `mapstructure:"ws_rate"`      // WebSocket upgrades per second (default: 2)
	WSBurst     int     `mapstructure:"ws_burst"`     // WebSocket burst capacity (default: 10)
	WriteRate   float64 `mapstructure:"write_rate"`   // POST/PUT/DELETE per second (default: 20)
	WriteBurst  int     `mapstructure:"write_burst"`  // Write burst capacity (default: 40)
	ReadRate    float64 `mapstructure:"read_rate"`    // GET per second (default: 60)
	ReadBurst   int     `mapstructure:"read_burst"`   // Read burst capacity (default: 120)
	PublicRate  float64 `mapstructure:"public_rate"`  // /health, static per second (default: 10)
	PublicBurst int     `mapstructure:"public_burst"` // Public burst capacity (default: 20)
}

// Server port constants
const (
	DefaultServerPort     = 8770 // Development port
	DefaultGraphEventPort = 8780 // Event viewer port
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
}

// CodeGitHubConfig configures GitHub integration for code review
type CodeGitHubConfig struct {
	Token string `mapstructure:"token"`
}

// AxConfig configures the attestation query system
type AxConfig struct {
	DefaultActor string `mapstructure:"default_actor"`
}

// PluginConfig configures the domain plugin system
type PluginConfig struct {
	Enabled     []string              `mapstructure:"enabled"`      // Allowlist of enabled plugins: bare names or repo URLs (see EnabledPlugin)
	Paths       []string              `mapstructure:"paths"`        // Plugin search paths (e.g., ["~/.qntx/plugins", "./plugins"])
	AccessToken []AccessTokenRef      `mapstructure:"access_token"` // One credential per forge host, for private plugin repos
	Runtime     PluginRuntimeConfig   `mapstructure:"runtime"`      // Runtime configuration
	WebSocket   PluginWebSocketConfig `mapstructure:"websocket"`    // WebSocket configuration
}

// AccessTokenRef points at the credential for one forge host.
//
// The host is a value, not a key. The config keyspace is dot-delimited, and
// every forge host contains a dot — as a key, "github.com" is indistinguishable
// from a nested table. Keeping it a value leaves nothing to split.
type AccessTokenRef struct {
	Host string `mapstructure:"host"` // Forge host, e.g. "github.com"
	Ref  string `mapstructure:"ref"`  // ssm:// or env: reference — a literal is rejected
}

// PluginRuntimeConfig configures plugin runtime environments
type PluginRuntimeConfig struct {
	TypeScriptRuntime string `mapstructure:"typescript_runtime"` // Path to TypeScript runtime (main.ts)
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
	ReclusterIntervalSeconds *int     `mapstructure:"recluster_interval_seconds"` // Pulse schedule interval for HDBSCAN re-clustering (nil = not scheduled, omit for default)
	ReprojectIntervalSeconds *int     `mapstructure:"reproject_interval_seconds"` // Pulse schedule interval for UMAP re-projection (nil = not scheduled, omit for default)
	MinClusterSize           int      `mapstructure:"min_cluster_size"`           // Minimum cluster size for HDBSCAN (default: 5)
	ClusterMatchThreshold    float64  `mapstructure:"cluster_match_threshold"`    // Cosine similarity threshold for stable cluster matching (default: 0.7)
	ProjectionMethods        []string `mapstructure:"projection_methods"`         // Dimensionality reduction methods: umap, tsne, pca (default: ["umap"])

	// Cluster labeling via LLM
	ClusterLabelIntervalSeconds *int   `mapstructure:"cluster_label_interval_seconds"` // Pulse schedule interval (nil = not scheduled, omit for default)
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
