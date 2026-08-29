/**
 * QNTX Type Definitions
 *
 * This module exports types from three sources:
 * 1. Proto (ADR-006) — the schedule and attestation surfaces
 * 2. Generated types (from Go source via typegen) — pulse/async only
 * 3. Frontend-only types (UI state, and the server package's wire shapes)
 *
 * IMPORTANT: Types in types/generated/typescript/ are auto-generated. Do not edit them directly.
 * Run `make types` to regenerate from Go source.
 */

// =============================================================================
// Generated types (from Go source - single source of truth)
// =============================================================================

// Async job types (pulse/async)
// Job uses ISO 8601 date strings (e.g., "2024-01-15T10:30:00Z")
// Frontend code parses these with new Date(job.created_at)
export type {
  Job,
  JobStatus,
  Progress,
  PulseState,
  ErrorCode,
  ErrorContext,
  QueueStats,
  SystemMetrics,
  WorkerPoolConfig,
} from '../../types/generated/typescript';

// The server package's REST payloads are declared here, not generated — see
// server.ts. The WebSocket messages are in websocket.ts for the same reason:
// each carries a literal type discriminator the Go struct spells as a string.
export type {
  ScheduledJobResponse,
  CreateScheduledJobRequest,
  UpdateScheduledJobRequest,
  ListScheduledJobsResponse,
  ChildJobInfo,
  JobChildrenResponse,
} from './server';

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
