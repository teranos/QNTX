/**
 * REST payloads the server package declares — see server/pulse_types.go.
 *
 * Scheduled jobs cross the wire as their own response shape, not as the
 * schedule proto: server/pulse_types.go:toScheduledJobResponse converts
 * schedule.Job into these fields. So they are declared here against those Go
 * structs, the way the WebSocket messages are declared in websocket.ts.
 */

/** server/pulse_types.go:ScheduledJobResponse */
export interface ScheduledJobResponse {
  id: string;
  handler_name: string;
  interval_seconds?: number;
  /** RFC3339 timestamp */
  next_run_at: string;
  /** RFC3339 timestamp */
  last_run_at?: string | null;
  /** Last async job ID */
  last_execution_id?: string;
  state: string;
  created_from_doc?: string;
  metadata?: string;
  /** RFC3339 timestamp */
  created_at: string;
  /** RFC3339 timestamp */
  updated_at: string;
}

/** server/pulse_types.go:CreateScheduledJobRequest */
export interface CreateScheduledJobRequest {
  /** Handler this schedule runs */
  handler_name: string;
  /** Execution interval in seconds */
  interval_seconds: number;
  /** Optional: ProseMirror document ID */
  created_from_doc?: string;
  /** Optional: JSON metadata */
  metadata?: string;
  /** Bypass deduplication checks (force execution) */
  force?: boolean;
}

/** server/pulse_types.go:UpdateScheduledJobRequest */
export interface UpdateScheduledJobRequest {
  /** active, paused, stopping, inactive */
  state?: string | null;
  interval_seconds?: number | null;
}

/** server/pulse_types.go:ListScheduledJobsResponse */
export interface ListScheduledJobsResponse {
  jobs: ScheduledJobResponse[];
  count?: number;
}

/** server/pulse_types.go:ChildJobInfo */
export interface ChildJobInfo {
  id: string;
  handler_name: string;
  source: string;
  status: string;
  progress_pct?: number;
  cost_estimate?: number;
  cost_actual?: number;
  error?: string;
  created_at: string;
  started_at?: string | null;
  completed_at?: string | null;
}

/** server/pulse_types.go:JobChildrenResponse — GET /api/pulse/jobs/:id/children */
export interface JobChildrenResponse {
  parent_job_id: string;
  children: ChildJobInfo[];
}
