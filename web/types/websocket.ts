/**
 * WebSocket message type definitions for QNTX Web UI
 *
 * This file imports Go-generated types from generated.ts and adds TS-specific
 * constructs (union types, handlers, client-only types).
 *
 * Go types are the source of truth - run `make typegen` to regenerate.
 */

import { GraphData } from './core';

// Re-export all Go-generated types
export type {
  // WebSocket messages (base types from Go - TS adds discriminated union refinement)
  QueryMessage,
  ProgressMessage,
  StatsMessage,
  CompleteMessage,
  UsageUpdateMessage,
  // Pulse API types
  CreateScheduledJobRequest,
  UpdateScheduledJobRequest,
  ScheduledJobResponse,
  ListScheduledJobsResponse,
  ErrorResponse,
  TaskInfo,
  StageInfo,
  JobStagesResponse,
  LogEntry,
  TaskLogsResponse,
  ChildJobInfo,
  JobChildrenResponse,
  // Async job types
  Job,
  JobStatus,
  Progress,
  PulseState,
} from './generated';

// Import raw Go types to extend with TypeScript discriminated unions
import type {
  DaemonStatusMessage as DaemonStatusMessageBase,
  LLMStreamMessage as LLMStreamMessageBase,
  PulseExecutionStartedMessage as PulseExecutionStartedMessageBase,
  PulseExecutionFailedMessage as PulseExecutionFailedMessageBase,
  PulseExecutionCompletedMessage as PulseExecutionCompletedMessageBase,
  PulseExecutionLogStreamMessage as PulseExecutionLogStreamMessageBase,
  StorageWarningMessage as StorageWarningMessageBase,
} from './generated';

// Re-export with refined type field for discriminated union support
export interface DaemonStatusMessage extends Omit<DaemonStatusMessageBase, 'type'> {
  type: 'daemon_status';
}
export interface LLMStreamMessage extends Omit<LLMStreamMessageBase, 'type'> {
  type: 'llm_stream';
}
export interface PulseExecutionStartedMessage extends Omit<PulseExecutionStartedMessageBase, 'type'> {
  type: 'pulse_execution_started';
}
export interface PulseExecutionFailedMessage extends Omit<PulseExecutionFailedMessageBase, 'type'> {
  type: 'pulse_execution_failed';
}
export interface PulseExecutionCompletedMessage extends Omit<PulseExecutionCompletedMessageBase, 'type'> {
  type: 'pulse_execution_completed';
}
export interface PulseExecutionLogStreamMessage extends Omit<PulseExecutionLogStreamMessageBase, 'type'> {
  type: 'pulse_execution_log_stream';
}
export interface StorageWarningMessage extends Omit<StorageWarningMessageBase, 'type'> {
  type: 'storage_warning';
}

// Import Job for use in this file
import type { Job } from './generated';

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
// Client-Only Message Types (not from Go)
// ============================================================================

export interface ReloadMessage extends BaseMessage {
  type: 'reload';
  reason?: string;
}

export interface BackendStatusMessage extends BaseMessage {
  type: 'backend_status';
  status: 'connected' | 'disconnected' | 'error';
  message?: string;
}

export interface VersionMessage extends BaseMessage {
  type: 'version';
  version: string;
  commit: string;
  build_time?: string;
  go_version?: string;
}

export interface ErrorMessage extends BaseMessage {
  type: 'error';
  error: string;
  code?: string;
  details?: unknown;
}

export interface JobUpdateMessage extends BaseMessage {
  type: 'job_update';
  job: Job;
  metadata?: {
    graph_query?: string;
    [key: string]: unknown;
  };
  action?: 'created' | 'updated' | 'completed' | 'failed' | 'cancelled';
}

export interface JobDetailsMessage extends BaseMessage {
  type: 'job_details';
  job: Job;
  jobs?: Job[];
  total?: number;
  page?: number;
}

export interface ImportProgressMessage extends BaseMessage {
  type: 'import_progress';
  stage: string;
  current: number;
  total: number;
  message?: string;
}

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

export interface ParseRequestMessage extends BaseMessage {
  type: 'parse_request';
  query: string;
  line: number;
  cursor: number;
  requestId?: string;
}

export interface ParseResponseMessage extends BaseMessage {
  type: 'parse_response';
  tokens: SemanticToken[];
  diagnostics: Diagnostic[];
  parse_state?: unknown;
  requestId?: string;
}

export interface GraphDataMessage extends BaseMessage {
  type: 'graph_data';
  data: GraphData;
  query?: string;
  execution_time?: number;
}

export interface LogsMessage extends BaseMessage {
  type: 'logs';
  logs: Array<{
    timestamp: number;
    level: 'info' | 'warn' | 'error' | 'debug';
    message: string;
    source?: string;
  }>;
}

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
// Parse Components
// ============================================================================

export interface SemanticToken {
  text: string;
  semantic_type: string;
  range: Range;
  modifiers?: string[];
}

export interface Range {
  start: Position;
  end: Position;
}

export interface Position {
  line: number;
  column: number;
  offset: number;
}

export interface Diagnostic {
  message: string;
  severity: 'error' | 'warning' | 'info' | 'hint';
  range: Range;
  source?: string;
  code?: string;
  suggestions?: string[];
  related_information?: DiagnosticRelatedInformation[];
}

export interface DiagnosticRelatedInformation {
  location: Range;
  message: string;
}

// ============================================================================
// Union Types
// ============================================================================

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
  | ParseRequestMessage
  | ParseResponseMessage
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

export type MessageHandler<T extends BaseMessage = BaseMessage> = (data: T) => void | Promise<void>;

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
  usage_update?: MessageHandler<BaseMessage>;
  parse_response?: MessageHandler<ParseResponseMessage>;
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

export interface WebSocketState {
  connected: boolean;
  connecting: boolean;
  url: string;
  reconnectAttempts: number;
  lastError?: string;
  lastMessageTime?: number;
}

export interface WebSocketConfig {
  url: string;
  reconnect?: boolean;
  reconnectDelay?: number;
  maxReconnectAttempts?: number;
  heartbeatInterval?: number;
  messageTimeout?: number;
}

// ============================================================================
// Type Aliases (backward compatibility)
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
