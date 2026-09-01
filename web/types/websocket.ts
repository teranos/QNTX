/**
 * WebSocket message type definitions for QNTX Web UI
 *
 * The messages the server broadcasts are declared here, against the Go structs
 * in server/types.go. Payload types that already exist in proto or in typegen's
 * remaining packages are imported rather than restated.
 */

import type { Attestation } from '../ts/generated/proto/plugin/grpc/protocol/atsstore';
import type { GlyphFired } from '../ts/generated/proto/glyph/proto/events';
import type { RichSearchResultsMessage as ProtoRichSearchResultsMessage } from '../ts/generated/proto/plugin/grpc/protocol/server';
import type { Job, JobStatus } from './async';
import type { Message as SystemCapabilities } from './syscap';

// Re-export Job for convenience
export type { Job, JobStatus };

// ============================================================================
// Message Type Discriminators
// ============================================================================

/**
 * All possible WebSocket message types
 */
export type MessageType =
  | 'reload'
  | 'backend_status'
  | 'job_update'
  | 'daemon_status'
  | 'llm_stream'
  | 'version'
  | 'logs'
  | 'import_progress'
  | 'import_stats'
  | 'import_complete'
  | 'ix_progress'
  | 'ix_error'
  | 'ix_complete'
  | 'usage_update'
  | 'parse_response'
  | 'parse_request'
  | 'job_details'
  | 'query'
  | 'error'
  | 'pulse_execution_started'
  | 'pulse_execution_failed'
  | 'pulse_execution_completed'
  | 'pulse_execution_log_stream'
  | 'storage_warning'
  | 'storage_eviction'
  | 'plugin_health'
  | 'system_capabilities'
  | 'watcher_match'
  | 'watcher_error'
  | 'glyph_fired'
  | 'watcher_queue_status'
  | 'database_stats'
  | 'rich_search_results'


// ============================================================================
// Base Message Interface
// ============================================================================

/**
 * Base interface for all WebSocket messages
 */
export interface BaseMessage {
  type: MessageType;
  timestamp?: number;
  id?: string;
}

// ============================================================================
// Server Broadcast Messages
// The wire shape the server writes — see server/types.go. Declared here rather
// than derived from a generator, so the discriminator is a literal and the
// message is one declaration instead of a base plus an Omit.
// ============================================================================

/**
 * Daemon status update (server/types.go:DaemonStatusMessage)
 */
export interface DaemonStatusMessage {
  type: 'daemon_status';
  running: boolean;
  active_jobs: number;
  queued_jobs: number;
  /** CPU/processing load (0-100) */
  load_percent: number;
  budget_daily: number;
  budget_weekly: number;
  budget_monthly: number;
  budget_daily_limit: number;
  budget_weekly_limit: number;
  budget_monthly_limit: number;
  /**
   * Aggregate spend (local + non-stale peers). Matches what CheckBudget()
   * enforces. Falls back to local spend when no peers are configured.
   */
  budget_daily_aggregate: number;
  budget_weekly_aggregate: number;
  budget_monthly_aggregate: number;
  /** Number of non-stale peers included */
  peer_count: number;
  /** Cluster limits (averaged across all nodes). 0 = not configured. */
  cluster_daily_limit: number;
  cluster_weekly_limit: number;
  cluster_monthly_limit: number;
  /** "running", "draining", "stopped" */
  server_state: string;
  timestamp: number;
}

/**
 * Job update notification (server/types.go:JobUpdateMessage)
 */
export interface JobUpdateMessage {
  type: 'job_update';
  job: Job;
  metadata: Record<string, unknown>;
  // Additional frontend-only fields
  action?: 'created' | 'updated' | 'completed' | 'failed' | 'cancelled';
}

/**
 * A candidate token from the top-k distribution
 * (server/types.go:LLMTokenCandidate)
 */
export interface LLMTokenCandidate {
  id: number;
  text: string;
  prob: number;
}

/**
 * Snapshot of the token distribution after a sampler stage
 * (server/types.go:SamplerStageSignal)
 */
export interface SamplerStageSignal {
  /** "logits", "top_k", "top_p", "temp", etc. */
  name: string;
  /** Tokens remaining with nonzero probability */
  active_count: number;
  /** P(top token) after this stage */
  top1_prob: number;
  /** Shannon entropy after this stage */
  entropy: number;
  top_k?: LLMTokenCandidate[];
}

/**
 * Per-token inference signal data (server/types.go:LLMTokenSignal)
 */
export interface LLMTokenSignal {
  /** P(chosen) from raw distribution */
  confidence: number;
  /** Shannon entropy in bits */
  entropy: number;
  /** P(top1) - P(top2) */
  top_gap: number;
  top_k?: LLMTokenCandidate[];
  /** Full softmax distribution (vocab_size floats) */
  full_distribution?: number[];
  /** Per-stage snapshots through the sampler chain */
  sampler_stages?: SamplerStageSignal[];
}

/**
 * LLM streaming output (server/types.go:LLMStreamMessage)
 */
export interface LLMStreamMessage {
  type: 'llm_stream';
  job_id: string;
  /** Optional task ID within job (for sub-tasks) */
  task_id?: string;
  content: string;
  done: boolean;
  model?: string;
  /** Current stage (e.g., "extraction") */
  stage?: string;
  error?: string;
  signal?: LLMTokenSignal | null;
  // Usage — populated on the final (done=true) chunk only
  prompt_tokens?: number;
  completion_tokens?: number;
  total_tokens?: number;
}

/**
 * Pulse execution started (server/types.go:PulseExecutionStartedMessage)
 */
export interface PulseExecutionStartedMessage {
  type: 'pulse_execution_started';
  scheduled_job_id: string;
  execution_id: string;
  handler_name: string;
  timestamp: number;
}

/**
 * Pulse execution failed (server/types.go:PulseExecutionFailedMessage)
 */
export interface PulseExecutionFailedMessage {
  type: 'pulse_execution_failed';
  scheduled_job_id: string;
  execution_id: string;
  handler_name: string;
  error_message: string;
  /** Structured error details from cockroachdb/errors */
  error_details: string[];
  /** How long before failure */
  duration_ms: number;
  timestamp: number;
}

/**
 * Pulse execution completed (server/types.go:PulseExecutionCompletedMessage)
 */
export interface PulseExecutionCompletedMessage {
  type: 'pulse_execution_completed';
  scheduled_job_id: string;
  execution_id: string;
  handler_name: string;
  async_job_id: string;
  result_summary: string;
  duration_ms: number;
  timestamp: number;
}

/**
 * Pulse execution log stream (server/types.go:PulseExecutionLogStreamMessage)
 */
export interface PulseExecutionLogStreamMessage {
  type: 'pulse_execution_log_stream';
  scheduled_job_id: string;
  execution_id: string;
  log_chunk: string;
  timestamp: number;
}

// ============================================================================
// WebSocket-Only Types (not generated from Go)
// ============================================================================

/**
 * Reload command - triggers UI refresh
 */
export interface ReloadMessage extends BaseMessage {
  type: 'reload';
  reason?: string;
}

/**
 * Backend status update
 */
export interface BackendStatusMessage extends BaseMessage {
  type: 'backend_status';
  status: 'connected' | 'disconnected' | 'error';
  message?: string;
}

/**
 * Version information
 */
export interface VersionMessage extends BaseMessage {
  type: 'version';
  version: string;
  commit: string;
  build_time?: string;
  go_version?: string;
}

/**
 * Error message
 */
export interface ErrorMessage extends BaseMessage {
  type: 'error';
  error: string;
  code?: string;
  details?: unknown;
}

/**
 * Job details response
 */
export interface JobDetailsMessage extends BaseMessage {
  type: 'job_details';
  job: Job;
  jobs?: Job[];
  total?: number;
  page?: number;
}

/**
 * Import progress update
 */
export interface ImportProgressMessage extends BaseMessage {
  type: 'import_progress';
  stage: string;
  current: number;
  total: number;
  message?: string;
}

/**
 * Import statistics
 */
export interface ImportStatsMessage extends BaseMessage {
  type: 'import_stats';
  imported?: number;
  skipped?: number;
  failed?: number;
  duration?: number;
  contacts?: number;
  attestations?: number;
  companies?: number;
}

/**
 * Import completion
 */
export interface ImportCompleteMessage extends BaseMessage {
  type: 'import_complete';
  success: boolean;
  message: string;
  stats?: {
    imported: number;
    skipped: number;
    failed: number;
  };
}

/**
 * IX operation progress
 */
export interface IXProgressMessage extends BaseMessage {
  type: 'ix_progress';
  operation: string;
  current: number;
  total: number;
  message?: string;
  event: {
    type: string;
    timestamp: string;
    data?: Record<string, unknown>;
  };
}

/**
 * IX operation error
 */
export interface IXErrorMessage extends BaseMessage {
  type: 'ix_error';
  operation: string;
  error: string;
  details?: unknown;
  event: {
    type: string;
    timestamp: string;
    data?: Record<string, unknown>;
  };
}

/**
 * IX operation complete
 */
export interface IXCompleteMessage extends BaseMessage {
  type: 'ix_complete';
  operation: string;
  success: boolean;
  result?: unknown;
  event: {
    type: string;
    timestamp: string;
    data?: Record<string, unknown>;
  };
}

/**
 * Usage update (matches server format from server/types.go:UsageUpdateMessage)
 */
export interface UsageUpdateMessage extends BaseMessage {
  type: 'usage_update';
  total_cost: number;    // Total cost in last 24h
  requests: number;      // Total requests
  success: number;       // Successful requests
  tokens: number;        // Total tokens used
  models: number;        // Unique models used
  since: string;         // Time period (e.g., "24h")
}

/**
 * Parse request (sent from frontend)
 */
export interface ParseRequestMessage extends BaseMessage {
  type: 'parse_request';
  query: string;
  line: number;
  cursor: number;
  requestId?: string;
}

/**
 * Parse response (received from backend)
 */
export interface ParseResponseMessage extends BaseMessage {
  type: 'parse_response';
  tokens: SemanticToken[];
  diagnostics: Diagnostic[];
  parse_state?: unknown;
  requestId?: string;
}

/**
 * Query request (sent from frontend)
 */
export interface QueryMessage extends BaseMessage {
  type: 'query';
  query: string;
  verbosity?: number;
  options?: {
    limit?: number;
    offset?: number;
    format?: string;
  };
}


/**
 * Log message entry from the backend
 */
export interface LogEntry {
  level: 'DEBUG' | 'INFO' | 'WARN' | 'ERROR';
  timestamp: string;
  logger: string;
  message: string;
  fields?: Record<string, unknown>;
}

/**
 * Log messages batch (matches server format: { type: "logs", data: { messages: [...] } })
 */
export interface LogsMessage extends BaseMessage {
  type: 'logs';
  data: {
    messages: LogEntry[];
  };
}

/**
 * Storage eviction notification when attestations are deleted due to limits
 */
export interface StorageEvictionMessage extends BaseMessage {
  type: 'storage_eviction';
  event_type: string;
  actor: string;
  context: string;
  entity: string;
  deletions_count: number;
  message: string;
}

/**
 * Plugin health update notification
 * Sent when plugin state changes (pause/resume) or health check fails
 */
export interface PluginHealthMessage extends BaseMessage {
  type: 'plugin_health';
  name: string;
  healthy: boolean;
  state: 'running' | 'paused' | 'stopped';
  message: string;
}

/**
 * System capabilities notification (from server/syscap/types.go:Message)
 * Sent once on WebSocket connection to inform client of available optimizations
 */
export interface SystemCapabilitiesMessage extends Omit<SystemCapabilities, 'type'> {
  type: 'system_capabilities';
}

/**
 * Watcher match notification - sent when a watcher matches a new attestation
 */
export interface WatcherMatchMessage extends BaseMessage {
  type: 'watcher_match';
  watcher_id: string;
  attestation: Attestation;
  score?: number;
  target_glyph_id?: string;
  timestamp: number;
}

/**
 * Glyph fired notification — sent when a meld-edge subscription triggers glyph execution.
 * Fields from proto GlyphFired + WebSocket type discriminator.
 */
export interface GlyphFiredMessage extends Omit<BaseMessage, 'timestamp'>, GlyphFired {
  type: 'glyph_fired';
}

/**
 * Watcher error notification - sent when a watcher encounters an error
 */
export interface WatcherErrorMessage extends BaseMessage {
  type: 'watcher_error';
  watcher_id: string;
  error: string;
  details?: string[];
  severity: string;
  timestamp: number;
}

/**
 * Per-watcher execution stats carried in queue status broadcasts
 * (server/types.go:WatcherBroadcastStats)
 */
export interface WatcherBroadcastStats {
  fire_count: number;
  error_count: number;
  /** Unix seconds, 0 = never */
  last_fired_at?: number;
  last_error?: string;
}

/**
 * Watcher queue status (server/types.go:WatcherQueueStatusMessage)
 * Broadcast every 5s while queue is non-empty
 */
export interface WatcherQueueStatusMessage {
  type: 'watcher_queue_status';
  total_queued: number;
  per_watcher: Record<string, number>;
  /** meld-edge watcher ID → target glyph ID */
  target_glyphs?: Record<string, string>;
  watcher_stats?: Record<string, WatcherBroadcastStats>;
  oldest_age_seconds: number;
  timestamp: number;
}

/**
 * Database statistics response
 */
export interface DatabaseStatsMessage extends BaseMessage {
  type: 'database_stats';
  path: string;
  storage_backend?: string;
  storage_optimized?: boolean;
  storage_version?: string;
  total_attestations: number;
  unique_actors: number;
  unique_subjects: number;
  unique_contexts: number;
  rich_fields?: unknown[];
  distillation?: { [key: string]: any };
}

/**
 * Rich search results response — extends proto-generated type with WS discriminator (ADR-006)
 */
export interface RichSearchResultsMessage extends ProtoRichSearchResultsMessage, BaseMessage {
  type: 'rich_search_results';
}

// ============================================================================
// Type Definitions for Parse Components
// ============================================================================

/**
 * Semantic token for syntax highlighting
 */
export interface SemanticToken {
  text: string;
  semantic_type: string;
  range: Range;
  modifiers?: string[];
}

/**
 * Text range
 */
export interface Range {
  start: Position;
  end: Position;
}

/**
 * Text position
 */
export interface Position {
  line: number;
  column: number;
  offset: number;
}

/**
 * Diagnostic message (error, warning, etc.)
 */
export interface Diagnostic {
  message: string;
  severity: 'error' | 'warning' | 'info' | 'hint';
  range: Range;
  source?: string;
  code?: string;
  suggestions?: string[];
  related_information?: DiagnosticRelatedInformation[];
}

/**
 * Related diagnostic information
 */
export interface DiagnosticRelatedInformation {
  location: Range;
  message: string;
}

// ============================================================================
// Union Types
// ============================================================================

/**
 * All possible WebSocket messages
 */
export type WebSocketMessage =
  | ReloadMessage
  | BackendStatusMessage
  | DaemonStatusMessage
  | JobUpdateMessage
  | JobDetailsMessage
  | LLMStreamMessage
  | VersionMessage
  | ErrorMessage
  | ImportProgressMessage
  | ImportStatsMessage
  | ImportCompleteMessage
  | IXProgressMessage
  | IXErrorMessage
  | IXCompleteMessage
  | UsageUpdateMessage
  | ParseRequestMessage
  | ParseResponseMessage
  | QueryMessage
  | LogsMessage
  | PulseExecutionStartedMessage
  | PulseExecutionFailedMessage
  | PulseExecutionCompletedMessage
  | PulseExecutionLogStreamMessage
  | StorageEvictionMessage
  | PluginHealthMessage
  | SystemCapabilitiesMessage
  | WatcherMatchMessage
  | WatcherErrorMessage
  | GlyphFiredMessage
  | WatcherQueueStatusMessage
  | DatabaseStatsMessage
  | RichSearchResultsMessage

// ============================================================================
// Message Handler Types
// ============================================================================

/**
 * Generic message handler
 */
export type MessageHandler<T extends BaseMessage = BaseMessage> = (data: T) => void | Promise<void>;

/**
 * Map of message handlers by type
 */
export interface MessageHandlers {
  reload?: MessageHandler<ReloadMessage>;
  backend_status?: MessageHandler<BackendStatusMessage>;
  daemon_status?: MessageHandler<DaemonStatusMessage>;
  job_update?: MessageHandler<JobUpdateMessage>;
  job_details?: MessageHandler<JobDetailsMessage>;
  llm_stream?: MessageHandler<LLMStreamMessage>;
  version?: MessageHandler<VersionMessage>;
  error?: MessageHandler<ErrorMessage>;
  logs?: MessageHandler<LogsMessage>;
  import_progress?: MessageHandler<ImportProgressMessage>;
  import_stats?: MessageHandler<ImportStatsMessage>;
  import_complete?: MessageHandler<ImportCompleteMessage>;
  ix_progress?: MessageHandler<IXProgressMessage>;
  ix_error?: MessageHandler<IXErrorMessage>;
  ix_complete?: MessageHandler<IXCompleteMessage>;
  usage_update?: MessageHandler<UsageUpdateMessage>;
  parse_response?: MessageHandler<ParseResponseMessage>;
  query?: MessageHandler<QueryMessage>;
  pulse_execution_started?: MessageHandler<PulseExecutionStartedMessage>;
  pulse_execution_failed?: MessageHandler<PulseExecutionFailedMessage>;
  pulse_execution_completed?: MessageHandler<PulseExecutionCompletedMessage>;
  pulse_execution_log_stream?: MessageHandler<PulseExecutionLogStreamMessage>;
  storage_eviction?: MessageHandler<StorageEvictionMessage>;
  plugin_health?: MessageHandler<PluginHealthMessage>;
  system_capabilities?: MessageHandler<SystemCapabilitiesMessage>;
  watcher_match?: MessageHandler<WatcherMatchMessage>;
  watcher_error?: MessageHandler<WatcherErrorMessage>;
  glyph_fired?: MessageHandler<GlyphFiredMessage>;
  watcher_queue_status?: MessageHandler<WatcherQueueStatusMessage>;
  database_stats?: MessageHandler<DatabaseStatsMessage>;
  rich_search_results?: MessageHandler<RichSearchResultsMessage>;
}

// ============================================================================
// WebSocket Connection Types
// ============================================================================

/**
 * WebSocket connection state
 */
export interface WebSocketState {
  connected: boolean;
  connecting: boolean;
  url: string;
  reconnectAttempts: number;
  lastError?: string;
  lastMessageTime?: number;
}

/**
 * WebSocket configuration
 */
export interface WebSocketConfig {
  url: string;
  reconnect?: boolean;
  reconnectDelay?: number;
  maxReconnectAttempts?: number;
  heartbeatInterval?: number;
  messageTimeout?: number;
}

// ============================================================================
// Type Aliases for Data Payloads (for backwards compatibility)
// ============================================================================

export type ImportProgressData = ImportProgressMessage;
export type ImportStatsData = ImportStatsMessage;
export type ImportCompleteData = ImportCompleteMessage;
export type IxProgressData = IXProgressMessage;
export type IxErrorData = IXErrorMessage;
export type IxCompleteData = IXCompleteMessage;
export type LLMStreamData = LLMStreamMessage;
export type JobUpdateData = JobUpdateMessage;
export type JobDetailsData = JobDetailsMessage;
