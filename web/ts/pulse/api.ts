/**
 * API client for Pulse Schedules
 *
 * Provides methods to interact with /api/pulse/schedules endpoints
 */

import { log, SEG } from "../logger";
import { handleError } from "../error-handler";
import type {
  ScheduledJobResponse,
  CreateScheduledJobRequest,
  UpdateScheduledJobRequest,
  ListScheduledJobsResponse,
} from "./types.ts";

/**
 * Get base URL for Pulse API - resolves at call time to ensure __BACKEND_URL__ is available
 */
function getBaseUrl(): string {
    const backendUrl = (window as any).__BACKEND_URL__ || '';
    const baseUrl = `${backendUrl}/api/pulse/schedules`;

    if (backendUrl) {
        log.debug(SEG.PULSE, 'Backend URL configured:', backendUrl);
    } else {
        log.debug(SEG.PULSE, 'Using same-origin backend');
    }
    log.debug(SEG.PULSE, 'Full URL:', baseUrl);

    return baseUrl;
}

/**
 * List all scheduled jobs
 */
export async function listScheduledJobs(): Promise<ScheduledJobResponse[]> {
  const response = await fetch(getBaseUrl());
  if (!response.ok) {
    throw new Error(`Failed to list scheduled jobs: ${response.statusText}`);
  }
  const data: ListScheduledJobsResponse = await response.json();
  return data.jobs;
}

/**
 * Get a specific scheduled job by ID
 */
export async function getScheduledJob(id: string): Promise<ScheduledJobResponse> {
  const response = await fetch(`${getBaseUrl()}/${id}`);
  if (!response.ok) {
    throw new Error(`Failed to get scheduled job: ${response.statusText}`);
  }
  return response.json();
}

/**
 * Create a new scheduled job
 */
export async function createScheduledJob(
  request: CreateScheduledJobRequest
): Promise<ScheduledJobResponse> {
  const baseUrl = getBaseUrl();
  log.debug(SEG.PULSE, 'Creating scheduled job:', request);

  const response = await fetch(baseUrl, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });

  log.debug(SEG.PULSE, 'Response:', {
    status: response.status,
    statusText: response.statusText,
    url: response.url,
    headers: Object.fromEntries(response.headers.entries())
  });

  if (!response.ok) {
    const responseText = await response.text();
    log.error(SEG.PULSE, 'Error response body:', responseText);

    let errorMessage = response.statusText;
    let errorDetails: string[] | undefined;

    try {
      const errorJson = JSON.parse(responseText);
      errorMessage = errorJson.error || errorJson.message || response.statusText;
      errorDetails = errorJson.details; // Preserve structured error details
    } catch (error: unknown) {
      handleError(error, 'Failed to parse API error response', { context: SEG.PULSE, silent: true });
      // Response wasn't JSON, use raw text
      errorMessage = responseText || response.statusText;
    }

    // Create error with details attached
    const err = new Error(`Failed to create scheduled job: ${errorMessage}`) as Error & { details?: string[] };
    err.details = errorDetails;
    throw err;
  }

  const job = await response.json();
  log.debug(SEG.PULSE, 'Created job response:', {
    id: job.id,
    created_from_doc: job.created_from_doc,
    hasCreatedFromDoc: !!job.created_from_doc
  });

  return job;
}

/**
 * Update a scheduled job (pause/resume, change interval)
 */
export async function updateScheduledJob(
  id: string,
  request: UpdateScheduledJobRequest
): Promise<ScheduledJobResponse> {
  const baseUrl = getBaseUrl();
  const url = `${baseUrl}/${id}`;

  log.debug(SEG.PULSE, 'Updating scheduled job:', { id, request, url });

  const response = await fetch(url, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });

  log.debug(SEG.PULSE, 'Update response:', {
    status: response.status,
    statusText: response.statusText,
    url: response.url,
  });

  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(`Failed to update scheduled job: ${error.error || response.statusText}`);
  }

  return response.json();
}

/**
 * Delete a scheduled job (sets to inactive state)
 */
export async function deleteScheduledJob(id: string): Promise<void> {
  const response = await fetch(`${getBaseUrl()}/${id}`, {
    method: "DELETE",
  });

  if (!response.ok) {
    const error = await response.json().catch(() => ({ error: response.statusText }));
    throw new Error(`Failed to delete scheduled job: ${error.error || response.statusText}`);
  }
}

/**
 * Pause a scheduled job
 */
export async function pauseScheduledJob(id: string): Promise<ScheduledJobResponse> {
  return updateScheduledJob(id, { state: "paused" });
}

/**
 * Resume a scheduled job
 */
export async function resumeScheduledJob(id: string): Promise<ScheduledJobResponse> {
  return updateScheduledJob(id, { state: "active" });
}

/**
 * Create a one-time force trigger job (bypasses deduplication)
 * Uses interval_seconds: 0 to indicate one-time execution
 * Accepts either ats_code or handler_name for handler-only schedules
 */
export async function forceTriggerJob(atsCode: string, handlerName?: string): Promise<ScheduledJobResponse> {
  log.debug(SEG.PULSE, 'Force triggering job:', atsCode || handlerName);

  return createScheduledJob({
    ats_code: atsCode,
    handler_name: handlerName,
    interval_seconds: 0, // One-time execution
    force: true, // Bypass deduplication
  });
}
