/**
 * QNTX Type Definitions
 *
 * A shape the node and the browser both speak is declared in proto and
 * generated from there (ADR-006). This module gathers those with the
 * frontend-only types — UI state, editor, git — that no wire carries.
 *
 * Run `make proto` to regenerate.
 */

// =============================================================================
// Generated from proto — the single source of truth for a wire shape
// =============================================================================

// What the scheduler answers with over HTTP. The gRPC forms of the same
// concepts live beside these in schedule.proto and carry numeric timestamps;
// these carry RFC3339, which is what the JSON API sends.
export type {
  ScheduledJobResponse,
  CreateScheduledJobRequest,
  UpdateScheduledJobRequest,
  ListScheduledJobsResponse,
  ChildJobInfo,
  JobChildrenResponse,
  ErrorResponse,
} from '../ts/generated/proto/plugin/grpc/protocol/schedule';

// A job as the browser receives it, and what the node says about itself while
// jobs run. Job uses RFC3339 strings, parsed with new Date(job.created_at).
export type {
  AsyncJob as Job,
  AsyncJobProgress as Progress,
  AsyncJobPulseState as PulseState,
  DaemonStatusMessage,
  JobUpdateMessage,
  LLMStreamMessage,
  PulseExecutionStartedMessage,
  PulseExecutionFailedMessage,
  PulseExecutionCompletedMessage,
  PulseExecutionLogStreamMessage,
} from '../ts/generated/proto/plugin/grpc/protocol/server';

export type { JobStatus } from './websocket';

// Execution and task logging, declared in schedule.proto. Go holds these as
// aliases of the protocol package, so there is one declaration and no mirror.
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
