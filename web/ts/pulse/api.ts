/**
 * API client for Pulse Schedules
 *
 * Provides methods to interact with /api/pulse/schedules endpoints
 */

import { debugLog, debugError } from "../debug.ts";
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
        debugLog('%c[Pulse API] DEV MODE', 'background: #00ff00; color: #000; font-weight: bold; padding: 4px 8px;', `Backend: ${backendUrl}`);
    } else {
        debugLog('%c[Pulse API] PRODUCTION', 'background: #0099ff; color: #fff; font-weight: bold; padding: 4px 8px;', 'Using same-origin');
    }
    debugLog('[Pulse API] Full URL:', baseUrl);

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
  debugLog('[Pulse API] Creating scheduled job:', request);

  const response = await fetch(baseUrl, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });

  debugLog('[Pulse API] Response:', {
    status: response.status,
    statusText: response.statusText,
    url: response.url,
    headers: Object.fromEntries(response.headers.entries())
  });

  if (!response.ok) {
    const responseText = await response.text();
    debugError('[Pulse API] Error response body:', responseText);

    let errorMessage = response.statusText;
    try {
      const errorJson = JSON.parse(responseText);
      errorMessage = errorJson.error || errorJson.message || response.statusText;
    } catch (e) {
      // Response wasn't JSON, use raw text
      errorMessage = responseText || response.statusText;
    }

    throw new Error(`Failed to create scheduled job: ${errorMessage}`);
  }

  const job = await response.json();
  debugLog('[Pulse API] Created job response:', {
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

  debugLog('[Pulse API] Updating scheduled job:', { id, request, url });

  const response = await fetch(url, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  });

  debugLog('[Pulse API] Update response:', {
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
 */
export async function forceTriggerJob(atsCode: string): Promise<ScheduledJobResponse> {
  debugLog('[Pulse API] Force triggering job:', atsCode);

  return createScheduledJob({
    ats_code: atsCode,
    interval_seconds: 0, // One-time execution
    force: true, // Bypass deduplication
  });
}
