/**
 * Pulse Execution Types - Re-exports from generated types
 *
 * Type definitions are imported from generated types.
 * This file only contains frontend-only types and re-exports.
 */

// Import types for local use
import type { ExecutionStatus as ExecutionStatusType } from '../../types';

// Re-export types from central location for convenience
export type {
  PulseExecution,
  ExecutionStatus,
  ListExecutionsResponse,
  TaskInfo,
  StageInfo,
  JobStagesResponse,
  TaskLogEntry as LogEntry,
  TaskLogsResponse,
  ChildJobInfo,
  JobChildrenResponse,
} from '../../types';

/**
 * Frontend-only: Parameters for listing executions
 * Not generated because this is client-side API params structure
 */
export interface ListExecutionsParams {
  limit?: number; // Default 50, max 100
  offset?: number; // Default 0
  status?: ExecutionStatusType; // Filter by status
}
