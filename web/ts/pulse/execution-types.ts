/**
 * TypeScript types for Pulse Execution History
 *
 * Matches the Go API contracts from internal/server/pulse_execution_handlers.go
 */

export type ExecutionStatus = "running" | "completed" | "failed";

export interface PulseExecution {
  id: string;
  scheduled_job_id: string;
  async_job_id?: string;
  status: ExecutionStatus;
  started_at: string; // RFC3339 timestamp
  completed_at?: string; // RFC3339 timestamp
  duration_ms?: number;
  logs?: string;
  result_summary?: string;
  error_message?: string;
  created_at: string; // RFC3339 timestamp
  updated_at: string; // RFC3339 timestamp
}

export interface ListExecutionsResponse {
  executions: PulseExecution[];
  count: number;
  total: number;
  has_more: boolean;
}

export interface ListExecutionsParams {
  limit?: number; // Default 50, max 100
  offset?: number; // Default 0
  status?: ExecutionStatus; // Filter by status
}

// Task Logging Types (matches internal/server/pulse_handlers.go)

export interface TaskInfo {
  task_id: string;
  log_count: number;
}

export interface StageInfo {
  stage: string;
  tasks: TaskInfo[];
}

export interface JobStagesResponse {
  job_id: string;
  stages: StageInfo[];
}

export interface LogEntry {
  timestamp: string; // RFC3339
  level: string; // "info", "warn", "error", "debug"
  message: string;
  metadata?: Record<string, any>;
}

export interface TaskLogsResponse {
  task_id: string;
  logs: LogEntry[];
}

// Child Job Types (matches internal/server/pulse_handlers.go)

export interface ChildJobInfo {
  id: string;
  handler_name: string;
  source: string;
  status: string;
  progress_pct: number;
  cost_estimate: number;
  cost_actual: number;
  error?: string;
  created_at: string; // RFC3339
  started_at?: string; // RFC3339
  completed_at?: string; // RFC3339
}

export interface JobChildrenResponse {
  parent_job_id: string;
  children: ChildJobInfo[];
}
