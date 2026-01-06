/**
 * WebSocket message type definitions for QNTX Web UI
 *
 * This file imports shared types from the generated typegen output to avoid drift,
 * and defines websocket-specific types that aren't generated from Go.
 */

import { GraphData } from './core';

// Import generated types from Go source (single source of truth)
import {
  Job,
  JobStatus,
} from '../../types/generated/typescript/async';

import {
  DaemonStatusMessage as GeneratedDaemonStatusMessage,
  JobUpdateMessage as GeneratedJobUpdateMessage,
  LLMStreamMessage as GeneratedLLMStreamMessage,
  StorageWarningMessage as GeneratedStorageWarningMessage,
  PulseExecutionStartedMessage as GeneratedPulseExecutionStartedMessage,
  PulseExecutionFailedMessage as GeneratedPulseExecutionFailedMessage,
  PulseExecutionCompletedMessage as GeneratedPulseExecutionCompletedMessage,
  PulseExecutionLogStreamMessage as GeneratedPulseExecutionLogStreamMessage,
} from '../../types/generated/typescript/server';

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
  | 'graph_data'
  | 'error'
  | 'pulse_execution_started'
  | 'pulse_execution_failed'
  | 'pulse_execution_completed'
  | 'pulse_execution_log_stream'
  | 'storage_warning'
  | 'storage_eviction'
  | 'plugin_health'
  | 'system_capabilities'
  | 'webscraper_request'
  | 'webscraper_response'
  | 'webscraper_progress';

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
// Re-exported Generated Types (with literal type discrimination)
// These types are generated from Go source - see server/types.go
// ============================================================================

/**
 * Daemon status update (from server/types.go:DaemonStatusMessage)
 */
export interface DaemonStatusMessage extends Omit<GeneratedDaemonStatusMessage, 'type'> {
  type: 'daemon_status';
}

/**
 * Job update notification (from server/types.go:JobUpdateMessage)
 */
export interface JobUpdateMessage extends Omit<GeneratedJobUpdateMessage, 'type' | 'job'> {
  type: 'job_update';
  job: Job;
  // Additional frontend-only fields
  action?: 'created' | 'updated' | 'completed' | 'failed' | 'cancelled';
}

/**
 * LLM streaming output (from server/types.go:LLMStreamMessage)
 */
export interface LLMStreamMessage extends Omit<GeneratedLLMStreamMessage, 'type'> {
  type: 'llm_stream';
  // Additional frontend-only fields for usage tracking
  usage?: {
    prompt_tokens?: number;
    completion_tokens?: number;
    total_tokens?: number;
  };
}

/**
 * Storage warning (from server/types.go:StorageWarningMessage)
 */
export interface StorageWarningMessage extends Omit<GeneratedStorageWarningMessage, 'type'> {
  type: 'storage_warning';
}

/**
 * Pulse execution started (from server/types.go:PulseExecutionStartedMessage)
 */
export interface PulseExecutionStartedMessage extends Omit<GeneratedPulseExecutionStartedMessage, 'type'> {
  type: 'pulse_execution_started';
}

/**
 * Pulse execution failed (from server/types.go:PulseExecutionFailedMessage)
 */
export interface PulseExecutionFailedMessage extends Omit<GeneratedPulseExecutionFailedMessage, 'type'> {
  type: 'pulse_execution_failed';
}

/**
 * Pulse execution completed (from server/types.go:PulseExecutionCompletedMessage)
 */
export interface PulseExecutionCompletedMessage extends Omit<GeneratedPulseExecutionCompletedMessage, 'type'> {
  type: 'pulse_execution_completed';
}

/**
 * Pulse execution log stream (from server/types.go:PulseExecutionLogStreamMessage)
 */
export interface PulseExecutionLogStreamMessage extends Omit<GeneratedPulseExecutionLogStreamMessage, 'type'> {
  type: 'pulse_execution_log_stream';
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
    data?: Record<string, any>;
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
    data?: Record<string, any>;
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
    data?: Record<string, any>;
  };
}

/**
 * Usage update
 */
export interface UsageUpdateMessage extends BaseMessage {
  type: 'usage_update';
  daily: {
    used: number;
    limit: number;
    percentage: number;
  };
  monthly: {
    used: number;
    limit: number;
    percentage: number;
  };
  costs?: {
    daily: number;
    monthly: number;
    currency: string;
  };
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
 * Graph data response
 */
export interface GraphDataMessage extends BaseMessage {
  type: 'graph_data';
  data: GraphData;
  query?: string;
  execution_time?: number;
}

/**
 * Log messages
 */
export interface LogsMessage extends BaseMessage {
  type: 'logs';
  logs: Array<{
    timestamp: number;
    level: 'info' | 'warn' | 'error' | 'debug';
    message: string;
    source?: string;
  }>;
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
 * System capabilities notification
 * Sent once on WebSocket connection to inform client of available optimizations
 */
export interface SystemCapabilitiesMessage extends BaseMessage {
  type: 'system_capabilities';
  fuzzy_backend: 'go' | 'rust';
  fuzzy_optimized: boolean;
}

/**
 * Webscraper request (sent from frontend)
 */
export interface WebscraperRequestMessage extends BaseMessage {
  type: 'webscraper_request';
  data: {
    url: string;
    javascript: boolean;
    wait_ms: number;
    extract_links: boolean;
    extract_images: boolean;
  };
}

/**
 * Webscraper response (received from backend)
 */
export interface WebscraperResponseMessage extends BaseMessage {
  type: 'webscraper_response';
  url: string;
  title?: string;
  description?: string;
  meta_description?: string;
  content?: string;
  links?: string[];
  images?: string[];
  error?: string;
}

/**
 * Webscraper progress update
 */
export interface WebscraperProgressMessage extends BaseMessage {
  type: 'webscraper_progress';
  message?: string;
  progress?: number;
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
  | GraphDataMessage
  | LogsMessage
  | PulseExecutionStartedMessage
  | PulseExecutionFailedMessage
  | PulseExecutionCompletedMessage
  | PulseExecutionLogStreamMessage
  | StorageWarningMessage
  | StorageEvictionMessage
  | PluginHealthMessage
  | SystemCapabilitiesMessage
  | WebscraperRequestMessage
  | WebscraperResponseMessage
  | WebscraperProgressMessage;

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
  graph_data?: MessageHandler<GraphDataMessage>;
  pulse_execution_started?: MessageHandler<PulseExecutionStartedMessage>;
  pulse_execution_failed?: MessageHandler<PulseExecutionFailedMessage>;
  pulse_execution_completed?: MessageHandler<PulseExecutionCompletedMessage>;
  pulse_execution_log_stream?: MessageHandler<PulseExecutionLogStreamMessage>;
  storage_warning?: MessageHandler<StorageWarningMessage>;
  storage_eviction?: MessageHandler<StorageEvictionMessage>;
  plugin_health?: MessageHandler<PluginHealthMessage>;
  system_capabilities?: MessageHandler<SystemCapabilitiesMessage>;
  webscraper_request?: MessageHandler<WebscraperRequestMessage>;
  webscraper_response?: MessageHandler<WebscraperResponseMessage>;
  webscraper_progress?: MessageHandler<WebscraperProgressMessage>;
  _default?: MessageHandler<BaseMessage>;
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
