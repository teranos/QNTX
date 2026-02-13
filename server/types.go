package server

import (
	"time"

	"github.com/teranos/QNTX/pulse/async"
)

const (
	// MaxClients is the maximum number of concurrent WebSocket clients
	MaxClients = 100
	// MaxClientMessageQueueSize is the size of per-client message queues
	MaxClientMessageQueueSize = 256
	// ShutdownTimeout is how long to wait for graceful shutdown
	// Set to 60s to accommodate:
	// - WorkerPool.Stop() can take up to 20s for checkpoint completion (configurable via WorkerPoolConfig.WorkerStopTimeout)
	// - Additional time for other goroutines (WebSocket, config watcher, etc.)
	ShutdownTimeout = 60 * time.Second
)

// DaemonState represents the activity level of the daemon for adaptive polling
type DaemonState int

const (
	DaemonIdle   DaemonState = iota // No jobs, no recent activity
	DaemonActive                    // Jobs running or queued
	DaemonBusy                      // High load (>60%)
)

// ServerState represents the server lifecycle state for Opening/Closing Phase 4
type ServerState int

const (
	ServerStateRunning  ServerState = iota // Normal operation
	ServerStateDraining                    // Graceful shutdown in progress
	ServerStateStopped                     // Shutdown complete
)

// cachedDaemonStatus tracks the last broadcast status to detect changes
type cachedDaemonStatus struct {
	activeJobs    int
	queuedJobs    int
	loadPercent   float64
	budgetDaily   float64
	budgetWeekly  float64
	budgetMonthly float64
}

type cachedUsageStats struct {
	totalCost float64
	requests  int
	success   int
	tokens    int
	models    int
}

// QueryMessage represents a client message
type QueryMessage struct {
	Type          string  `json:"type"`           // "query", "clear", "ping", "set_verbosity", "set_graph_limit", "upload", "daemon_control", "pulse_config_update", "job_control", "visibility", "vidstream_init", "vidstream_frame", "rich_search"
	Query         string  `json:"query"`          // The Ax query text (can be multi-line)
	Line          int     `json:"line"`           // Current line number (for multi-line support)
	Cursor        int     `json:"cursor"`         // Cursor position
	Verbosity     int     `json:"verbosity"`      // Verbosity level for set_verbosity
	GraphLimit    int     `json:"graph_limit"`    // Graph node limit for set_graph_limit
	Filename      string  `json:"filename"`       // For upload messages
	FileType      string  `json:"fileType"`       // For upload messages: "linkedin", "vcf", etc.
	Data          string  `json:"data"`           // For upload messages: base64 encoded file content
	Action        string  `json:"action"`         // For daemon_control/job_control/visibility messages: "start", "stop", "pause", "resume", "details", "toggle_node_type", "toggle_isolated"
	DailyBudget   float64 `json:"daily_budget"`   // For pulse_config_update messages
	WeeklyBudget  float64 `json:"weekly_budget"`  // For pulse_config_update messages
	MonthlyBudget float64 `json:"monthly_budget"` // For pulse_config_update messages
	JobID         string  `json:"job_id"`         // For job_control messages
	NodeType      string  `json:"node_type"`      // For visibility messages: node type to toggle
	Hidden        bool    `json:"hidden"`         // For visibility messages: whether to hide the node type/isolated nodes
	// VidStream fields (for vidstream_init and vidstream_frame messages)
	ModelPath           string  `json:"model_path"`           // For vidstream_init: path to ONNX model
	ConfidenceThreshold float32 `json:"confidence_threshold"` // For vidstream_init: detection confidence threshold
	NMSThreshold        float32 `json:"nms_threshold"`        // For vidstream_init: NMS IoU threshold
	FrameData           []byte  `json:"frame_data"`           // For vidstream_frame: raw frame bytes (RGBA)
	Width               int     `json:"width"`                // For vidstream_frame: frame width
	Height              int     `json:"height"`               // For vidstream_frame: frame height
	Format              string  `json:"format"`               // For vidstream_frame: "rgba8", "rgb8", etc.
	// Watcher fields (for watcher_upsert messages)
	WatcherID    string `json:"watcher_id"`    // For watcher_upsert: ID of watcher (generated if empty)
	WatcherQuery string `json:"watcher_query"` // For watcher_upsert: AX query string
	WatcherName  string `json:"watcher_name"`  // For watcher_upsert: Human-readable watcher name
	Enabled      bool   `json:"enabled"`       // For watcher_upsert: Whether watcher is enabled
}

// ProgressMessage represents an import progress message
type ProgressMessage struct {
	Type    string `json:"type"`    // "import_progress"
	Current int    `json:"current"` // Current item being processed
	Total   int    `json:"total"`   // Total items to process
	Message string `json:"message"` // Status message
}

// StatsMessage represents import statistics
type StatsMessage struct {
	Type         string `json:"type"`         // "import_stats"
	Contacts     int    `json:"contacts"`     // Number of contacts imported
	Attestations int    `json:"attestations"` // Number of attestations created
	Companies    int    `json:"companies"`    // Number of companies found
}

// CompleteMessage represents import completion
type CompleteMessage struct {
	Type    string `json:"type"`    // "import_complete"
	Message string `json:"message"` // Completion message
}

// UsageUpdateMessage represents AI usage statistics update
type UsageUpdateMessage struct {
	Type      string  `json:"type"`       // "usage_update"
	TotalCost float64 `json:"total_cost"` // Total cost in last 24h
	Requests  int     `json:"requests"`   // Total requests
	Success   int     `json:"success"`    // Successful requests
	Tokens    int     `json:"tokens"`     // Total tokens used
	Models    int     `json:"models"`     // Unique models used
	Since     string  `json:"since"`      // Time period (e.g., "24h")
	Timestamp int64   `json:"timestamp"`  // Unix timestamp
}

// JobUpdateMessage represents async IX job update
type JobUpdateMessage struct {
	Type     string                 `json:"type"`                    // "job_update"
	Job      *async.Job             `json:"job" tstype:"Job | null"` // Full job details (from pulse/async)
	Metadata map[string]interface{} `json:"metadata"`                // Additional metadata
}

// DaemonStatusMessage represents daemon status update
type DaemonStatusMessage struct {
	Type               string  `json:"type"`                 // "daemon_status"
	Running            bool    `json:"running"`              // Is daemon running
	ActiveJobs         int     `json:"active_jobs"`          // Number of active jobs
	QueuedJobs         int     `json:"queued_jobs"`          // Number of queued jobs
	LoadPercent        float64 `json:"load_percent"`         // CPU/processing load (0-100)
	BudgetDaily        float64 `json:"budget_daily"`         // Daily budget spent
	BudgetWeekly       float64 `json:"budget_weekly"`        // Weekly budget spent
	BudgetMonthly      float64 `json:"budget_monthly"`       // Monthly budget spent
	BudgetDailyLimit   float64 `json:"budget_daily_limit"`   // Daily budget limit (config)
	BudgetWeeklyLimit  float64 `json:"budget_weekly_limit"`  // Weekly budget limit (config)
	BudgetMonthlyLimit float64 `json:"budget_monthly_limit"` // Monthly budget limit (config)
	ServerState        string  `json:"server_state"`         // Opening/Closing Phase 4: "running", "draining", "stopped"
	Timestamp          int64   `json:"timestamp"`            // Unix timestamp
}

// LLMStreamMessage represents streaming LLM output
type LLMStreamMessage struct {
	Type    string `json:"type"`              // "llm_stream"
	JobID   string `json:"job_id"`            // Job ID this stream belongs to
	TaskID  string `json:"task_id,omitempty"` // Optional task ID within job (for sub-tasks)
	Content string `json:"content"`           // Token/chunk of text
	Done    bool   `json:"done"`              // True when stream is complete
	Model   string `json:"model,omitempty"`   // Model name
	Stage   string `json:"stage,omitempty"`   // Current stage (e.g., "extraction")
	Error   string `json:"error,omitempty"`   // Error message if streaming failed
}

// PulseExecutionStartedMessage represents a Pulse execution that just started
type PulseExecutionStartedMessage struct {
	Type           string `json:"type"`             // "pulse_execution_started"
	ScheduledJobID string `json:"scheduled_job_id"` // Job that's executing
	ExecutionID    string `json:"execution_id"`     // Execution record ID
	ATSCode        string `json:"ats_code"`         // ATS code being executed
	Timestamp      int64  `json:"timestamp"`        // Unix timestamp
}

// PulseExecutionFailedMessage represents a Pulse execution that failed
type PulseExecutionFailedMessage struct {
	Type           string   `json:"type"`             // "pulse_execution_failed"
	ScheduledJobID string   `json:"scheduled_job_id"` // Job that failed
	ExecutionID    string   `json:"execution_id"`     // Execution record ID
	ATSCode        string   `json:"ats_code"`         // ATS code that was executed
	ErrorMessage   string   `json:"error_message"`    // Error description
	ErrorDetails   []string `json:"error_details"`    // Structured error details from cockroachdb/errors
	DurationMs     int      `json:"duration_ms"`      // How long before failure
	Timestamp      int64    `json:"timestamp"`        // Unix timestamp
}

// PulseExecutionCompletedMessage represents a Pulse execution that completed successfully
type PulseExecutionCompletedMessage struct {
	Type           string `json:"type"`             // "pulse_execution_completed"
	ScheduledJobID string `json:"scheduled_job_id"` // Job that completed
	ExecutionID    string `json:"execution_id"`     // Execution record ID
	ATSCode        string `json:"ats_code"`         // ATS code that was executed
	AsyncJobID     string `json:"async_job_id"`     // Created async job ID
	ResultSummary  string `json:"result_summary"`   // Brief result description
	DurationMs     int    `json:"duration_ms"`      // Execution duration
	Timestamp      int64  `json:"timestamp"`        // Unix timestamp
}

// PulseExecutionLogStreamMessage represents live log output from a running execution
type PulseExecutionLogStreamMessage struct {
	Type           string `json:"type"`             // "pulse_execution_log_stream"
	ScheduledJobID string `json:"scheduled_job_id"` // Job being executed
	ExecutionID    string `json:"execution_id"`     // Execution record ID
	LogChunk       string `json:"log_chunk"`        // Log text chunk
	Timestamp      int64  `json:"timestamp"`        // Unix timestamp
}

// StorageWarningMessage represents a bounded storage warning for approaching limits
type StorageWarningMessage struct {
	Type          string  `json:"type"`            // "storage_warning"
	Actor         string  `json:"actor"`           // Actor approaching limit
	Context       string  `json:"context"`         // Context approaching limit
	Current       int     `json:"current"`         // Current attestation count
	Limit         int     `json:"limit"`           // Configured limit
	FillPercent   float64 `json:"fill_percent"`    // Percentage full (0.0-1.0)
	TimeUntilFull string  `json:"time_until_full"` // Human-readable time until hitting limit
	Timestamp     int64   `json:"timestamp"`       // Unix timestamp
}

// PluginHealthMessage represents a plugin health status update
// Broadcast when plugin state changes (pause/resume) or health check fails
type PluginHealthMessage struct {
	Type      string `json:"type"`      // "plugin_health"
	Name      string `json:"name"`      // Plugin name
	Healthy   bool   `json:"healthy"`   // Current health status
	State     string `json:"state"`     // "running", "paused", "stopped"
	Message   string `json:"message"`   // Status message
	Timestamp int64  `json:"timestamp"` // Unix timestamp
}

// WatcherMatchMessage represents a watcher match event
// Sent when an attestation matches a watcher's filter
type WatcherMatchMessage struct {
	Type        string      `json:"type"`        // "watcher_match"
	WatcherID   string      `json:"watcher_id"`  // ID of watcher that matched
	Attestation interface{} `json:"attestation"` // The matching attestation (types.As)
	Timestamp   int64       `json:"timestamp"`   // Unix timestamp
}

// GlyphFiredMessage wraps proto.GlyphFired with WebSocket type discriminator
type GlyphFiredMessage struct {
	Type          string `json:"type"`             // "glyph_fired"
	GlyphID       string `json:"glyph_id"`         // Target glyph that was executed
	AttestationID string `json:"attestation_id"`   // Triggering attestation ASID
	Status        string `json:"status"`           // "started", "success", "error"
	Error         string `json:"error,omitempty"`  // Error message when status is "error"
	Result        string `json:"result,omitempty"` // JSON-encoded execution result
	Timestamp     int64  `json:"timestamp"`        // Unix timestamp
}

// WatcherErrorMessage represents a watcher error (parsing failure, validation error, etc.)
// Sent when watcher creation/update fails
type WatcherErrorMessage struct {
	Type      string   `json:"type"`              // "watcher_error"
	WatcherID string   `json:"watcher_id"`        // ID of watcher that failed
	Error     string   `json:"error"`             // Error message
	Details   []string `json:"details,omitempty"` // Structured error context from errors.GetAllDetails()
	Severity  string   `json:"severity"`          // "error" or "warning"
	Timestamp int64    `json:"timestamp"`         // Unix timestamp
}
