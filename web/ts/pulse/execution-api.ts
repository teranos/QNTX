/**
 * API Client for Pulse Execution History
 *
 * Provides functions to fetch execution records, logs, and history
 */

import { log, SEG } from "../logger";
import { apiFetch } from "../api.ts";
import type {
  Execution,
  ListExecutionsResponse,
  ListExecutionsParams,
  JobStagesResponse,
  TaskLogsResponse,
  JobChildrenResponse,
} from "./execution-types.ts";

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

  const path = `/api/pulse/jobs/${jobId}/executions?${searchParams}`;
  log.debug(SEG.PULSE, "Listing executions:", { jobId, params });

  const response = await apiFetch(path);

  if (!response.ok) {
    const error = `Failed to list executions: ${response.statusText}`;
    log.error(SEG.PULSE, "List failed:", error);
    throw new Error(error);
  }

  const data = await response.json();
  log.debug(SEG.PULSE, "Listed executions:", {
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
  log.debug(SEG.PULSE, "Getting execution:", { executionId });

  const response = await apiFetch(`/api/pulse/executions/${executionId}`);

  if (!response.ok) {
    if (response.status === 404) {
      throw new Error("Execution not found");
    }
    const error = `Failed to get execution: ${response.statusText}`;
    log.error(SEG.PULSE, "Get failed:", error);
    throw new Error(error);
  }

  const data = await response.json();
  log.debug(SEG.PULSE, "Got execution:", {
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
  log.debug(SEG.PULSE, "Getting logs:", { executionId });

  const response = await apiFetch(`/api/pulse/executions/${executionId}/logs`);

  if (!response.ok) {
    if (response.status === 404) {
      throw new Error("No logs available");
    }
    const error = `Failed to get logs: ${response.statusText}`;
    log.error(SEG.PULSE, "Get logs failed:", error);
    throw new Error(error);
  }

  const logs = await response.text();
  log.debug(SEG.PULSE, "Got logs:", { length: logs.length });

  return logs;
}

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
  log.debug(SEG.PULSE, "Getting job stages:", { jobId });

  const response = await apiFetch(`/api/pulse/jobs/${jobId}/stages`);

  if (!response.ok) {
    if (response.status === 404) {
      // Return empty stages instead of throwing - job may not have logs yet
      log.debug(SEG.PULSE, "No stages found for job:", jobId);
      return { job_id: jobId, stages: [] };
    }
    const error = `Failed to get job stages: ${response.statusText}`;
    log.error(SEG.PULSE, "Get stages failed:", error);
    throw new Error(error);
  }

  const data = await response.json();
  log.debug(SEG.PULSE, "Got stages:", {
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
  log.debug(SEG.PULSE, "Getting task logs:", { taskId });

  const response = await apiFetch(`/api/pulse/tasks/${taskId}/logs`);

  if (!response.ok) {
    if (response.status === 404) {
      throw new Error("Task not found or has no logs");
    }
    const error = `Failed to get task logs: ${response.statusText}`;
    log.error(SEG.PULSE, "Get logs failed:", error);
    throw new Error(error);
  }

  const data = await response.json();
  log.debug(SEG.PULSE, "Got task logs:", {
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
  log.debug(SEG.PULSE, "Getting task logs for job:", { jobId, taskId });

  const response = await apiFetch(`/api/pulse/jobs/${jobId}/tasks/${taskId}/logs`);

  if (!response.ok) {
    if (response.status === 404) {
      throw new Error("Task not found or has no logs");
    }
    const error = `Failed to get task logs: ${response.statusText}`;
    log.error(SEG.PULSE, "Get logs failed:", error);
    throw new Error(error);
  }

  const data = await response.json();
  log.debug(SEG.PULSE, "Got task logs:", {
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
  log.debug(SEG.PULSE, "Getting child jobs:", { jobId });

  const response = await apiFetch(`/api/pulse/jobs/${jobId}/children`);

  if (!response.ok) {
    if (response.status === 404) {
      // Return empty children instead of throwing - job may not have children
      log.debug(SEG.PULSE, "No children found for job:", jobId);
      return { parent_job_id: jobId, children: [] };
    }
    const error = `Failed to get child jobs: ${response.statusText}`;
    log.error(SEG.PULSE, "Get children failed:", error);
    throw new Error(error);
  }

  const data = await response.json();
  log.debug(SEG.PULSE, "Got child jobs:", {
    parent_job_id: data.parent_job_id,
    child_count: data.children.length,
  });

  return data;
}
