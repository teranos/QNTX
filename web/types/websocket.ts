/**
 * WebSocket message type definitions for QNTX Web UI
 * Defines all message types exchanged between frontend and backend
 */

import { GraphData } from './core';
import { Job } from '../../types/generated/typescript';

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
  | 'storage_eviction';

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
// System Messages
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

// ============================================================================
// Git Messages
// ============================================================================



// ============================================================================
// Daemon & Job Messages
// ============================================================================

/**
 * Daemon status update
 */
export interface DaemonStatusMessage extends BaseMessage {
  type: 'daemon_status';
  running: boolean;
  active_jobs: number;
  load_percent: number;
  budget_daily?: number;
  budget_weekly?: number;
  budget_monthly?: number;
  budget_daily_limit?: number;
  budget_weekly_limit?: number;
  budget_monthly_limit?: number;
}

/**
 * Job update notification
 */
export interface JobUpdateMessage extends BaseMessage {
  type: 'job_update';
  job: Job;
  metadata?: {
    graph_query?: string;
    [key: string]: any;
  };
  action?: 'created' | 'updated' | 'completed' | 'failed' | 'cancelled';
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

// ============================================================================
// LLM Streaming Messages
// ============================================================================

/**
 * LLM streaming output
 */
export interface LLMStreamMessage extends BaseMessage {
  type: 'llm_stream';
  job_id?: string;
  task_id?: string;
  stage?: string;
  model?: string;
  content: string;
  done: boolean;
  error?: string;
  usage?: {
    prompt_tokens?: number;
    completion_tokens?: number;
    total_tokens?: number;
  };
}

// ============================================================================
// Import/Ingestion Messages
// ============================================================================

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

// ============================================================================
// IX (Index) Operation Messages
// ============================================================================

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

// ============================================================================
// Usage Messages
// ============================================================================

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

// ============================================================================
// Parse/Query Messages
// ============================================================================

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

// ============================================================================
// Log Messages
// ============================================================================

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
  | StorageEvictionMessage;

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

/**
 * Type alias for import progress data
 */
export type ImportProgressData = ImportProgressMessage;

/**
 * Type alias for import stats data
 */
export type ImportStatsData = ImportStatsMessage;

/**
 * Type alias for import complete data
 */
export type ImportCompleteData = ImportCompleteMessage;

/**
 * Type alias for IX progress data
 */
export type IxProgressData = IXProgressMessage;

// ============================================================================
// Pulse Execution Messages
// ============================================================================

/**
 * Pulse execution started notification
 */
export interface PulseExecutionStartedMessage extends BaseMessage {
  type: 'pulse_execution_started';
  scheduled_job_id: string;
  execution_id: string;
  ats_code: string;
  timestamp: number;
}

/**
 * Pulse execution failed notification
 */
export interface PulseExecutionFailedMessage extends BaseMessage {
  type: 'pulse_execution_failed';
  scheduled_job_id: string;
  execution_id: string;
  ats_code: string;
  error_message: string;
  duration_ms: number;
  timestamp: number;
}

/**
 * Pulse execution completed notification
 */
export interface PulseExecutionCompletedMessage extends BaseMessage {
  type: 'pulse_execution_completed';
  scheduled_job_id: string;
  execution_id: string;
  ats_code: string;
  async_job_id: string;
  result_summary: string;
  duration_ms: number;
  timestamp: number;
}

/**
 * Pulse execution log stream chunk
 */
export interface PulseExecutionLogStreamMessage extends BaseMessage {
  type: 'pulse_execution_log_stream';
  scheduled_job_id: string;
  execution_id: string;
  log_chunk: string;
  timestamp: number;
}

/**
 * Storage warning for approaching bounded storage limits
 */
export interface StorageWarningMessage extends BaseMessage {
  type: 'storage_warning';
  actor: string;
  context: string;
  current: number;
  limit: number;
  fill_percent: number;
  time_until_full: string;
  timestamp: number;
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

// ============================================================================
// Message Data Type Aliases
// ============================================================================

/**
 * Type alias for IX error data
 */
export type IxErrorData = IXErrorMessage;

/**
 * Type alias for IX complete data
 */
export type IxCompleteData = IXCompleteMessage;

/**
 * Type alias for LLM stream data
 */
export type LLMStreamData = LLMStreamMessage;

/**
 * Type alias for job update data
 */
export type JobUpdateData = JobUpdateMessage;

/**
 * Type alias for job details data
 */
export type JobDetailsData = JobDetailsMessage;