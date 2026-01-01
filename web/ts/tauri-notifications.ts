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

// Threshold for "long-running" job notifications (30 seconds)
const LONG_JOB_THRESHOLD_MS = 30000;

// Track when jobs started for duration-based notifications
const jobStartTimes = new Map<string, number>();

// Track last server state to detect transitions
let lastServerState: string | undefined = undefined;

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
        // Access the global Tauri API (injected by Tauri with withGlobalTauri: true)
        const tauri = (window as any).__TAURI__;
        if (!tauri?.core?.invoke) {
            console.warn('[tauri-notifications] Tauri API not available');
            return undefined;
        }
        return await tauri.core.invoke(command, args) as T;
    } catch (error) {
        console.warn(`[tauri-notifications] Failed to invoke ${command}:`, error);
        return undefined;
    }
}

/**
 * Track when a job starts running
 * Call this when a job transitions to 'running' status
 */
export function trackJobStart(jobId: string): void {
    jobStartTimes.set(jobId, Date.now());
}

/**
 * Get the duration a job has been running
 * Returns undefined if job wasn't tracked
 */
export function getJobDuration(jobId: string): number | undefined {
    const startTime = jobStartTimes.get(jobId);
    if (!startTime) return undefined;
    return Date.now() - startTime;
}

/**
 * Clean up tracking for a completed/failed job
 */
export function cleanupJobTracking(jobId: string): void {
    jobStartTimes.delete(jobId);
}

/**
 * Determine if a completed job should trigger a notification
 * Only notifies for long-running jobs to avoid spam
 */
export function shouldNotifyCompletion(jobId: string): boolean {
    const duration = getJobDuration(jobId);
    if (duration === undefined) return false;
    return duration >= LONG_JOB_THRESHOLD_MS;
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

    // Detect state transitions
    if (lastServerState !== currentState) {
        if (currentState === 'draining' && lastServerState !== 'draining') {
            // Server just entered draining mode
            await notifyServerDraining(status.active_jobs, status.queued_jobs);
        } else if (currentState === 'stopped' && lastServerState !== 'stopped') {
            // Server just stopped
            await notifyServerStopped();
        }
    }

    // Update tracked state
    lastServerState = currentState;
}
