/**
 * QNTX Type Definitions
 *
 * This module exports types from two sources:
 * 1. Generated types (from Go source via ats/typegen)
 * 2. Frontend-only types (UI state, D3 visualization, etc.)
 *
 * IMPORTANT: Types in generated/ are auto-generated. Do not edit them directly.
 * Run `make types` to regenerate from Go source.
 */

// =============================================================================
// Generated types (from Go source - single source of truth)
// =============================================================================

// Attestation types (ats/types)
export type {
  As,
  AsCommand,
  AxDebug,
  AxFilter,
  AxResult,
  AxSummary,
  CompletionItem,
  Conflict,
  OverFilter,
} from './generated/types';

// Async job types (pulse/async)
// Job uses ISO 8601 date strings (e.g., "2024-01-15T10:30:00Z")
// Frontend code parses these with new Date(job.created_at)
export type {
  Job,
  JobStatus,
  Progress,
  PulseState,
  BudgetStatus,
  ErrorCode,
  ErrorContext,
  QueueStats,
  SystemMetrics,
  WorkerPoolConfig,
} from './generated/async';

// Server/WebSocket message types (server)
export type {
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
  StorageWarningMessage,
  ErrorResponse,
} from './generated/server';

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
  Result,
  PaginatedResponse,
} from './core';

// Graph visualization types
export type {
  GraphData,
  Node,
  Link,
  GraphMeta,
  NodeTypeInfo,
  Transform,
  NodeType,
  GraphPhysics,
  GraphStyles,
  UIText,
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

// D3 graph types
export * from './d3-graph';

// LSP types
export * from './lsp';

// Configuration types
export * from './config';
