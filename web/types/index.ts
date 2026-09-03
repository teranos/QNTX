/**
 * QNTX Type Definitions
 *
 * This module exports types from two sources:
 * 1. Generated types (from Go source via ats/typegen)
 * 2. Frontend-only types (UI state, etc.)
 *
 * IMPORTANT: Types in types/generated/typescript/ are auto-generated. Do not edit them directly.
 * Run `make types` to regenerate from Go source.
 */

// =============================================================================
// Generated types (from Go source - single source of truth)
// =============================================================================

// All types are re-exported from the auto-generated barrel file
export type {
  // Async job types (pulse/async)
  // Job uses ISO 8601 date strings (e.g., "2024-01-15T10:30:00Z")
  // Frontend code parses these with new Date(job.created_at)
  Job,
  JobStatus,
  Progress,
  PulseState,
  ErrorCode,
  ErrorContext,
  QueueStats,
  SystemMetrics,
  WorkerPoolConfig,
  // Server/WebSocket message types (server)
  DaemonStatusMessage,
  JobUpdateMessage,
  LLMStreamMessage,
  UsageUpdateMessage,
  QueryMessage,
  ProgressMessage,
  CompleteMessage,
  StatsMessage,
  PulseExecutionStartedMessage,
  PulseExecutionFailedMessage,
  PulseExecutionCompletedMessage,
  PulseExecutionLogStreamMessage,
  ErrorResponse,
  // Scheduled job types (server)
  ScheduledJobResponse,
  CreateScheduledJobRequest,
  UpdateScheduledJobRequest,
  ListScheduledJobsResponse,
  ChildJobInfo,
  JobChildrenResponse,
} from '../../types/generated/typescript';

// Execution and task logging come from proto now (ADR-006). typegen no longer
// emits them, because Go declares them as aliases of the protocol package.
export type {
  Execution,
  ListExecutionsResponse,
  TaskInfo,
  StageInfo,
  JobStagesResponse,
  TaskLogsResponse,
} from '../ts/generated/proto/plugin/grpc/protocol/schedule';

// ServerLogEntry avoids the collision with core.ts LogEntry, which is the UI
// console's. This one is the task/execution log line.
export type { LogEntry as ServerLogEntry } from '../ts/generated/proto/plugin/grpc/protocol/schedule';

// The value sets come from schedule.proto. Spelling them out here is what let
// this union lose "deleted" without anything noticing.
import type {
  ScheduleState,
  ExecutionStatus as ExecutionStatusEnum,
} from '../ts/generated/proto/plugin/grpc/protocol/schedule';

export type ScheduledJobState = Exclude<keyof typeof ScheduleState, 'UNRECOGNIZED'>;
export type ExecutionStatus = Exclude<keyof typeof ExecutionStatusEnum, 'UNRECOGNIZED'>;

// =============================================================================
// Frontend-only types (not generated)
// =============================================================================

// Core UI types
export type {
  AppState,
  SessionData,
  LogEntry,
  ProgressEvent,
  PanelState,
  EditorState,
  LogMessage,
  LogBatchData,
  UIText,
  Result,
  PaginatedResponse,
} from './core';

// Git and AI types
export type {
  GitBranch,
  GitStatus,
  AIProvider,
} from './core';

// WebSocket infrastructure types
export type {
  MessageType,
  BaseMessage,
  WebSocketMessage,
  MessageHandler,
  MessageHandlers,
  WebSocketState,
  WebSocketConfig,
} from './websocket';

// Configuration types
export * from './config';
