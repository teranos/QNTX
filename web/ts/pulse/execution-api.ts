/**
 * API Client for Pulse Execution History
 *
 * Provides functions to fetch execution records, logs, and history
 */

import { debugLog, debugError } from "../debug.ts";
import type {
  Execution,
  ListExecutionsResponse,
  ListExecutionsParams,
  JobStagesResponse,
  TaskLogsResponse,
  JobChildrenResponse,
} from "./execution-types.ts";
import { formatRelativeTime as formatRelativeTimeUtil } from "../html-utils.ts";

/**
 * Get base URL for Pulse API endpoints
 */
function getBaseUrl(): string {
  const backendUrl = (window as any).__BACKEND_URL__ || "";
  return `${backendUrl}/api/pulse`;
}

/**
 * Wrapper for fetch with consistent error handling
 * @param url - URL to fetch
 * @param options - Fetch options
 * @returns Response object
 * @throws Error with user-friendly message on network or HTTP errors
 */
async function safeFetch(url: string, options?: RequestInit): Promise<Response> {
  try {
    const response = await fetch(url, options);
    return response;
  } catch (error) {
    // Network error (connection refused, DNS failure, etc.)
    debugError("[Execution API] Network error:", error);
    throw new Error('Network error: Unable to connect to server. Please check your connection.');
  }
}

/**
 * List executions for a scheduled job
 *
 * @param jobId - Scheduled job ID
 * @param params - Optional pagination and filtering parameters
 * @returns List of executions with pagination metadata
 */
export async function listExecutions(
  jobId: string,
  params: ListExecutionsParams = {}
): Promise<ListExecutionsResponse> {
  const { limit = 50, offset = 0, status } = params;

  const searchParams = new URLSearchParams({
    limit: String(limit),
    offset: String(offset),
  });

  if (status) {
    searchParams.set("status", status);
  }

  const url = `${getBaseUrl()}/jobs/${jobId}/executions?${searchParams}`;
  debugLog("[Execution API] Listing executions:", { jobId, params, url });

  const response = await safeFetch(url);

  if (!response.ok) {
    const error = `Failed to list executions: ${response.statusText}`;
    debugError("[Execution API] List failed:", error);
    throw new Error(error);
  }

  const data = await response.json();
  debugLog("[Execution API] Listed executions:", {
    count: data.count,
    total: data.total,
    has_more: data.has_more,
  });

  return data;
}

/**
 * Get execution details by ID
 *
 * @param executionId - Execution ID
 * @returns Execution details including logs and result summary
 */
export async function getExecution(
  executionId: string
): Promise<Execution> {
  const url = `${getBaseUrl()}/executions/${executionId}`;
  debugLog("[Execution API] Getting execution:", { executionId, url });

  const response = await safeFetch(url);

  if (!response.ok) {
    if (response.status === 404) {
      throw new Error("Execution not found");
    }
    const error = `Failed to get execution: ${response.statusText}`;
    debugError("[Execution API] Get failed:", error);
    throw new Error(error);
  }

  const data = await response.json();
  debugLog("[Execution API] Got execution:", {
    id: data.id,
    status: data.status,
    duration_ms: data.duration_ms,
  });

  return data;
}

/**
 * Get execution logs (plain text)
 *
 * @param executionId - Execution ID
 * @returns Raw log output as plain text
 */
export async function getExecutionLogs(
  executionId: string
): Promise<string> {
  const url = `${getBaseUrl()}/executions/${executionId}/logs`;
  debugLog("[Execution API] Getting logs:", { executionId, url });

  const response = await safeFetch(url);

  if (!response.ok) {
    if (response.status === 404) {
      throw new Error("No logs available");
    }
    const error = `Failed to get logs: ${response.statusText}`;
    debugError("[Execution API] Get logs failed:", error);
    throw new Error(error);
  }

  const logs = await response.text();
  debugLog("[Execution API] Got logs:", { length: logs.length });

  return logs;
}

/**
 * Format execution duration for display
 *
 * @param durationMs - Duration in milliseconds
 * @returns Human-readable duration string
 */
export function formatDuration(durationMs: number): string {
  if (durationMs < 1000) {
    return `${durationMs}ms`;
  }

  const seconds = Math.floor(durationMs / 1000);
  if (seconds < 60) {
    return `${seconds}s`;
  }

  const minutes = Math.floor(seconds / 60);
  const remainingSeconds = seconds % 60;
  return `${minutes}m ${remainingSeconds}s`;
}

/**
 * Format relative time for execution timestamps
 * Re-exported from html-utils for convenience
 */
export const formatRelativeTime = formatRelativeTimeUtil;

/**
 * Get status color class for styling
 *
 * @param status - Execution status (string from API)
 * @returns CSS class name for status color
 */
export function getStatusColorClass(status: string): string {
  switch (status) {
    case "running":
      return "status-running";
    case "completed":
      return "status-completed";
    case "failed":
      return "status-failed";
    default:
      return "";
  }
}

/**
 * Get stages and tasks for a job
 *
 * @param jobId - Async job ID (from pulse_execution.async_job_id)
 * @returns Hierarchical stages and tasks with log counts
 */
export async function getJobStages(jobId: string): Promise<JobStagesResponse> {
  const url = `${getBaseUrl()}/jobs/${jobId}/stages`;
  debugLog("[Execution API] Getting job stages:", { jobId, url });

  const response = await safeFetch(url);

  if (!response.ok) {
    if (response.status === 404) {
      // Return empty stages instead of throwing - job may not have logs yet
      debugLog("[Execution API] No stages found for job:", jobId);
      return { job_id: jobId, stages: [] };
    }
    const error = `Failed to get job stages: ${response.statusText}`;
    debugError("[Execution API] Get stages failed:", error);
    throw new Error(error);
  }

  const data = await response.json();
  debugLog("[Execution API] Got stages:", {
    job_id: data.job_id,
    stage_count: data.stages.length,
  });

  return data;
}

/**
 * Get logs for a specific task
 *
 * @param taskId - Task ID (e.g., CNT_abc123 or stage name for stage-level tasks)
 * @returns Log entries with timestamp, level, message, metadata
 */
export async function getTaskLogs(taskId: string): Promise<TaskLogsResponse> {
  const url = `${getBaseUrl()}/tasks/${taskId}/logs`;
  debugLog("[Execution API] Getting task logs:", { taskId, url });

  const response = await safeFetch(url);

  if (!response.ok) {
    if (response.status === 404) {
      throw new Error("Task not found or has no logs");
    }
    const error = `Failed to get task logs: ${response.statusText}`;
    debugError("[Execution API] Get logs failed:", error);
    throw new Error(error);
  }

  const data = await response.json();
  debugLog("[Execution API] Got task logs:", {
    task_id: data.task_id,
    log_count: data.logs.length,
  });

  return data;
}

/**
 * Get logs for a specific task within a job context
 *
 * @param jobId - Job ID containing the task
 * @param taskId - Task ID (e.g., "fetch_jd", "extract_requirements")
 * @returns Log entries with timestamp, level, message, metadata
 */
export async function getTaskLogsForJob(
  jobId: string,
  taskId: string
): Promise<TaskLogsResponse> {
  const url = `${getBaseUrl()}/jobs/${jobId}/tasks/${taskId}/logs`;
  debugLog("[Execution API] Getting task logs for job:", { jobId, taskId, url });

  const response = await safeFetch(url);

  if (!response.ok) {
    if (response.status === 404) {
      throw new Error("Task not found or has no logs");
    }
    const error = `Failed to get task logs: ${response.statusText}`;
    debugError("[Execution API] Get logs failed:", error);
    throw new Error(error);
  }

  const data = await response.json();
  debugLog("[Execution API] Got task logs:", {
    job_id: jobId,
    task_id: data.task_id,
    log_count: data.logs.length,
  });

  return data;
}

/**
 * Get child jobs for a parent async job
 *
 * @param jobId - Parent async job ID
 * @returns List of child jobs with status and progress
 */
export async function getJobChildren(
  jobId: string
): Promise<JobChildrenResponse> {
  const url = `${getBaseUrl()}/jobs/${jobId}/children`;
  debugLog("[Execution API] Getting child jobs:", { jobId, url });

  const response = await safeFetch(url);

  if (!response.ok) {
    if (response.status === 404) {
      // Return empty children instead of throwing - job may not have children
      debugLog("[Execution API] No children found for job:", jobId);
      return { parent_job_id: jobId, children: [] };
    }
    const error = `Failed to get child jobs: ${response.statusText}`;
    debugError("[Execution API] Get children failed:", error);
    throw new Error(error);
  }

  const data = await response.json();
  debugLog("[Execution API] Got child jobs:", {
    parent_job_id: data.parent_job_id,
    child_count: data.children.length,
  });

  return data;
}
