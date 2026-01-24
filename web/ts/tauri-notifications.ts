/**
 * Native desktop notifications via Tauri
 *
 * Provides OS-level notifications for job completion/failure when running
 * in the Tauri desktop app. Falls back silently in browser environments.
 *
 * Uses smart notification logic:
 * - Always notify on job failures
 * - Only notify on completion for long-running jobs (>30s)
 * - Track job durations to avoid notification spam
 */

import { invoke } from '@tauri-apps/api/core';

// Threshold for "long-running" job notifications (30 seconds)
const LONG_JOB_THRESHOLD_MS = 30000;

// Maximum number of jobs to track before cleanup (prevents unbounded growth)
const MAX_TRACKED_JOBS = 1000;

// Age after which stale job entries are cleaned up (2 hours)
const STALE_JOB_AGE_MS = 2 * 60 * 60 * 1000;

/**
 * Notification state manager
 * Encapsulates mutable state to avoid race conditions with multiple connections
 */
class NotificationState {
    private jobStartTimes = new Map<string, number>();
    private lastServerState: string | undefined = undefined;

    /**
     * Track when a job starts running
     */
    trackJobStart(jobId: string): void {
        // Enforce max size limit to prevent unbounded growth
        if (this.jobStartTimes.size >= MAX_TRACKED_JOBS) {
            this.cleanupStaleJobs();
        }
        this.jobStartTimes.set(jobId, Date.now());
    }

    /**
     * Get the duration a job has been running
     * Returns undefined if job wasn't tracked
     */
    getJobDuration(jobId: string): number | undefined {
        const startTime = this.jobStartTimes.get(jobId);
        if (!startTime) return undefined;
        return Date.now() - startTime;
    }

    /**
     * Clean up tracking for a completed/failed job
     */
    cleanupJob(jobId: string): void {
        this.jobStartTimes.delete(jobId);
    }

    /**
     * Clean up jobs that have been tracked for too long (likely stuck or orphaned)
     * Prevents memory leaks from jobs that never complete
     */
    private cleanupStaleJobs(): void {
        const now = Date.now();
        const staleJobs: string[] = [];

        for (const [jobId, startTime] of this.jobStartTimes.entries()) {
            if (now - startTime > STALE_JOB_AGE_MS) {
                staleJobs.push(jobId);
            }
        }

        if (staleJobs.length > 0) {
            console.warn(
                `[tauri-notifications] Cleaning up ${staleJobs.length} stale job(s)`,
                staleJobs
            );
            for (const jobId of staleJobs) {
                this.jobStartTimes.delete(jobId);
            }
        }
    }

    /**
     * Determine if a completed job should trigger a notification
     */
    shouldNotifyCompletion(jobId: string): boolean {
        const duration = this.getJobDuration(jobId);
        if (duration === undefined) return false;
        return duration >= LONG_JOB_THRESHOLD_MS;
    }

    /**
     * Get the last server state
     */
    getLastServerState(): string | undefined {
        return this.lastServerState;
    }

    /**
     * Update the last server state
     */
    setLastServerState(state: string): void {
        this.lastServerState = state;
    }
}

// Single instance for the application lifetime
// Note: This assumes a single WebSocket connection. If you need to support
// multiple connections, create separate instances per connection.
const notificationState = new NotificationState();

/**
 * Check if running in Tauri desktop environment
 */
export function isTauri(): boolean {
    return '__TAURI__' in window;
}

/**
 * Invoke a Tauri command if in Tauri environment
 * Returns a resolved promise with undefined if not in Tauri
 */
async function invokeIfTauri<T>(command: string, args: Record<string, unknown>): Promise<T | undefined> {
    if (!isTauri()) {
        return undefined;
    }

    try {
        return await invoke<T>(command, args);
    } catch (error: unknown) {
        console.warn(`[tauri-notifications] Failed to invoke ${command}:`, error);
        return undefined;
    }
}

/**
 * Track when a job starts running
 * Call this when a job transitions to 'running' status
 */
export function trackJobStart(jobId: string): void {
    notificationState.trackJobStart(jobId);
}

/**
 * Get the duration a job has been running
 * Returns undefined if job wasn't tracked
 */
export function getJobDuration(jobId: string): number | undefined {
    return notificationState.getJobDuration(jobId);
}

/**
 * Clean up tracking for a completed/failed job
 */
export function cleanupJobTracking(jobId: string): void {
    notificationState.cleanupJob(jobId);
}

/**
 * Determine if a completed job should trigger a notification
 * Only notifies for long-running jobs to avoid spam
 */
export function shouldNotifyCompletion(jobId: string): boolean {
    return notificationState.shouldNotifyCompletion(jobId);
}

/**
 * Send a notification for a completed job
 * Only sends if in Tauri environment
 */
export async function notifyJobCompleted(
    handlerName: string,
    jobId: string,
    durationMs?: number
): Promise<void> {
    await invokeIfTauri('notify_job_completed', {
        handlerName,
        jobId,
        durationMs: durationMs ?? null
    });
}

/**
 * Send a notification for a failed job
 * Only sends if in Tauri environment
 */
export async function notifyJobFailed(
    handlerName: string,
    jobId: string,
    error?: string
): Promise<void> {
    await invokeIfTauri('notify_job_failed', {
        handlerName,
        jobId,
        error: error ?? null
    });
}

/**
 * Send a notification for storage warning
 * Only sends if in Tauri environment
 */
export async function notifyStorageWarning(
    actor: string,
    fillPercent: number
): Promise<void> {
    await invokeIfTauri('notify_storage_warning', {
        actor,
        fillPercent
    });
}

/**
 * Handle a job update message and send appropriate notifications
 * This is the main entry point called from the WebSocket handler
 */
export async function handleJobNotification(job: {
    id: string;
    handler_name: string;
    status: string;
    error?: string;
}): Promise<void> {
    // Don't send notifications if not in Tauri
    if (!isTauri()) return;

    switch (job.status) {
        case 'running':
            // Track job start time for duration calculations
            trackJobStart(job.id);
            break;

        case 'completed': {
            const duration = getJobDuration(job.id);
            // Only notify for long-running jobs
            if (shouldNotifyCompletion(job.id)) {
                await notifyJobCompleted(job.handler_name, job.id, duration);
            }
            cleanupJobTracking(job.id);
            break;
        }

        case 'failed':
            // Always notify on failure
            await notifyJobFailed(job.handler_name, job.id, job.error);
            cleanupJobTracking(job.id);
            break;

        case 'cancelled':
            // Clean up but don't notify
            cleanupJobTracking(job.id);
            break;
    }
}

/**
 * Send a notification when server enters draining mode
 * Only sends if in Tauri environment
 */
export async function notifyServerDraining(
    activeJobs: number,
    queuedJobs: number
): Promise<void> {
    await invokeIfTauri('notify_server_draining', {
        activeJobs,
        queuedJobs
    });
}

/**
 * Send a notification when server stops
 * Only sends if in Tauri environment
 */
export async function notifyServerStopped(): Promise<void> {
    await invokeIfTauri('notify_server_stopped', {});
}

/**
 * Handle daemon status updates and send notifications for state changes
 * Called from WebSocket handler when daemon_status messages arrive
 */
export async function handleDaemonStatusNotification(status: {
    server_state?: string;
    active_jobs: number;
    queued_jobs: number;
}): Promise<void> {
    // Don't send notifications if not in Tauri
    if (!isTauri()) return;

    const currentState = status.server_state || 'running';
    const lastState = notificationState.getLastServerState();

    // Detect state transitions
    if (lastState !== currentState) {
        if (currentState === 'draining' && lastState !== 'draining') {
            // Server just entered draining mode
            await notifyServerDraining(status.active_jobs, status.queued_jobs);
        } else if (currentState === 'stopped' && lastState !== 'stopped') {
            // Server just stopped
            await notifyServerStopped();
        }
    }

    // Update tracked state
    notificationState.setLastServerState(currentState);
}

/**
 * Taskbar progress state for Windows (works on macOS/Linux too)
 * - none: Clear progress indicator
 * - normal: Show progress bar with percentage
 * - indeterminate: Show spinning/pulsing indicator (jobs queued but no progress info)
 * - paused: Show yellow/warning progress bar (Pulse daemon paused)
 * - error: Show red progress bar (job failed)
 */
export type TaskbarProgressState = 'none' | 'normal' | 'indeterminate' | 'paused' | 'error';

/**
 * Set taskbar progress indicator
 * Primary use: Windows taskbar progress bar, but also works on macOS dock
 *
 * @param state - Progress state: 'none' | 'normal' | 'indeterminate' | 'paused' | 'error'
 * @param progress - Optional percentage (0-100), only used for 'normal' state
 */
export async function setTaskbarProgress(
    state: TaskbarProgressState,
    progress?: number
): Promise<void> {
    await invokeIfTauri('set_taskbar_progress', {
        state,
        progress: progress !== undefined ? Math.round(progress) : null
    });
}

/**
 * Clear taskbar progress indicator
 */
export async function clearTaskbarProgress(): Promise<void> {
    await setTaskbarProgress('none');
}

/**
 * Show indeterminate progress (spinning indicator)
 * Use when jobs are queued/running but no specific progress percentage available
 */
export async function showTaskbarIndeterminate(): Promise<void> {
    await setTaskbarProgress('indeterminate');
}

/**
 * Show progress percentage
 * @param percent - Progress percentage (0-100)
 */
export async function showTaskbarProgress(percent: number): Promise<void> {
    await setTaskbarProgress('normal', Math.max(0, Math.min(100, percent)));
}

/**
 * Show paused state (yellow/warning indicator)
 * Use when Pulse daemon is paused
 */
export async function showTaskbarPaused(progress?: number): Promise<void> {
    await setTaskbarProgress('paused', progress);
}

/**
 * Show error state (red indicator)
 * Use when a job has failed
 */
export async function showTaskbarError(progress?: number): Promise<void> {
    await setTaskbarProgress('error', progress);
}

/**
 * Update taskbar based on current job queue state
 * Call this when job counts change to reflect overall progress
 *
 * @param activeJobs - Number of currently running jobs
 * @param queuedJobs - Number of queued jobs
 * @param isPaused - Whether Pulse daemon is paused
 * @param hasError - Whether any job recently failed
 */
export async function updateTaskbarFromJobState(
    activeJobs: number,
    queuedJobs: number,
    isPaused: boolean = false,
    hasError: boolean = false
): Promise<void> {
    if (!isTauri()) return;

    const totalJobs = activeJobs + queuedJobs;

    if (totalJobs === 0) {
        // No jobs - clear progress
        await clearTaskbarProgress();
    } else if (hasError) {
        // Show error state
        await showTaskbarError();
    } else if (isPaused) {
        // Show paused state
        await showTaskbarPaused();
    } else {
        // Jobs are processing - show indeterminate progress
        // (We don't have per-job progress, so use indeterminate)
        await showTaskbarIndeterminate();
    }
}
