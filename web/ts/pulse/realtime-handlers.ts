/**
 * Real-time WebSocket handlers for Pulse execution events
 *
 * Handles:
 * - Execution started notifications
 * - Execution failed notifications (with toast)
 * - Execution completed notifications
 * - Live log streaming
 * - ATS block execution state subscriptions
 *
 * Updates both:
 * - Pulse panel inline execution view (when job is expanded)
 * - Job detail panel full history (when viewing specific job)
 * - ATS code blocks (border color based on execution state)
 *
 * TODO(#30): Future real-time improvements:
 * - Add execution progress updates (percentage completion)
 * - Add live resource usage metrics (CPU, memory for running jobs)
 * - Add execution cancellation via WebSocket
 * - Add batch execution status updates (reduce message frequency)
 */

import { log, SEG } from '../logger';
import { handleError } from '../error-handler';

// ========================================================================
// ATS Block Execution State Subscriptions
// ========================================================================

/**
 * Execution state for ATS blocks
 */
export type ATSExecutionState = 'idle' | 'running' | 'completed' | 'failed';

/**
 * Subscriber callback for ATS execution state changes
 */
export type ATSExecutionStateCallback = (state: ATSExecutionState, executionId?: string) => void;

/**
 * Map of scheduled job ID -> subscribers
 */
const atsBlockSubscribers = new Map<string, Set<ATSExecutionStateCallback>>();

/**
 * Subscribe an ATS block to execution state updates for a scheduled job
 * @param scheduledJobId - The scheduled job ID to subscribe to
 * @param callback - Called when execution state changes
 * @returns Unsubscribe function
 */
export function subscribeATSBlock(
    scheduledJobId: string,
    callback: ATSExecutionStateCallback
): () => void {
    if (!atsBlockSubscribers.has(scheduledJobId)) {
        atsBlockSubscribers.set(scheduledJobId, new Set());
    }
    atsBlockSubscribers.get(scheduledJobId)!.add(callback);

    log.debug(SEG.PULSE, 'ATS Block subscribed to job:', scheduledJobId);

    // Return unsubscribe function
    return () => {
        const subscribers = atsBlockSubscribers.get(scheduledJobId);
        if (subscribers) {
            subscribers.delete(callback);
            if (subscribers.size === 0) {
                atsBlockSubscribers.delete(scheduledJobId);
            }
        }
        log.debug(SEG.PULSE, 'ATS Block unsubscribed from job:', scheduledJobId);
    };
}

/**
 * Notify all ATS block subscribers of a state change
 */
function notifyATSBlockSubscribers(
    scheduledJobId: string,
    state: ATSExecutionState,
    executionId?: string
): void {
    const subscribers = atsBlockSubscribers.get(scheduledJobId);
    if (subscribers) {
        log.debug(SEG.PULSE, 'ATS Block notifying', subscribers.size, 'subscribers for job:', scheduledJobId, 'state:', state);
        for (const callback of subscribers) {
            try {
                callback(state, executionId);
            } catch (error) {
                handleError(error, 'ATS Block subscriber callback error', { context: SEG.PULSE, silent: true });
            }
        }
    }
}
import {
    PulseExecutionStartedMessage,
    PulseExecutionFailedMessage,
    PulseExecutionCompletedMessage,
    PulseExecutionLogStreamMessage
} from '../../types/websocket';
import { toast } from '../components/toast';
import {
    dispatchExecutionStarted,
    dispatchExecutionCompleted,
    dispatchExecutionFailed,
    dispatchExecutionLog
} from './events';

// ========================================================================
// WebSocket event handlers
// ========================================================================

/**
 * Handle execution started notification
 * - Notifies ATS block subscribers to show running state
 * - Dispatches custom event for panels (pulse-panel, job-detail-panel)
 */
export function handlePulseExecutionStarted(data: PulseExecutionStartedMessage): void {
    log.debug(SEG.PULSE, 'Execution started:', {
        job_id: data.scheduled_job_id,
        execution_id: data.execution_id,
        ats_code: data.ats_code
    });

    // Notify ATS block subscribers
    notifyATSBlockSubscribers(data.scheduled_job_id, 'running', data.execution_id);

    // Dispatch custom event for type-safe cross-panel communication
    // Panels subscribe to these events directly (see pulse-panel.ts and job-detail-panel.ts)
    dispatchExecutionStarted({
        scheduledJobId: data.scheduled_job_id,
        executionId: data.execution_id,
        atsCode: data.ats_code,
        timestamp: data.timestamp
    });
}

/**
 * Handle execution failed notification
 * - Notifies ATS block subscribers to show failed state
 * - Dispatches custom event for panels
 * - Shows failure toast notification
 */
export function handlePulseExecutionFailed(data: PulseExecutionFailedMessage): void {
    log.debug(SEG.PULSE, 'Execution failed:', {
        job_id: data.scheduled_job_id,
        execution_id: data.execution_id,
        error: data.error_message,
        duration_ms: data.duration_ms
    });

    // Notify ATS block subscribers
    notifyATSBlockSubscribers(data.scheduled_job_id, 'failed', data.execution_id);

    // Dispatch custom event for type-safe cross-panel communication
    // Panels subscribe to these events directly (see pulse-panel.ts and job-detail-panel.ts)
    dispatchExecutionFailed({
        scheduledJobId: data.scheduled_job_id,
        executionId: data.execution_id,
        errorMessage: data.error_message,
        durationMs: data.duration_ms,
        atsCode: data.ats_code,
        timestamp: data.timestamp
    });

    // ALWAYS show failure toast
    toast.error(`Pulse job failed: ${data.ats_code}\n\nError: ${data.error_message}`);
}

/**
 * Handle execution completed notification
 * - Notifies ATS block subscribers to show completed state
 * - Dispatches custom event for panels
 */
export function handlePulseExecutionCompleted(data: PulseExecutionCompletedMessage): void {
    log.debug(SEG.PULSE, 'Execution completed:', {
        job_id: data.scheduled_job_id,
        execution_id: data.execution_id,
        async_job_id: data.async_job_id,
        duration_ms: data.duration_ms
    });

    // Notify ATS block subscribers
    notifyATSBlockSubscribers(data.scheduled_job_id, 'completed', data.execution_id);

    // Dispatch custom event for type-safe cross-panel communication
    // Panels subscribe to these events directly (see pulse-panel.ts and job-detail-panel.ts)
    dispatchExecutionCompleted({
        scheduledJobId: data.scheduled_job_id,
        executionId: data.execution_id,
        asyncJobId: data.async_job_id,
        resultSummary: data.result_summary,
        durationMs: data.duration_ms,
        timestamp: data.timestamp
    });
}

/**
 * Handle live log streaming
 * - Dispatches custom event for panels to handle log streaming
 */
export function handlePulseExecutionLogStream(data: PulseExecutionLogStreamMessage): void {
    log.debug(SEG.PULSE, 'Log chunk received:', {
        job_id: data.scheduled_job_id,
        execution_id: data.execution_id,
        chunk_length: data.log_chunk.length
    });

    // Dispatch custom event for type-safe cross-panel communication
    dispatchExecutionLog({
        scheduledJobId: data.scheduled_job_id,
        executionId: data.execution_id,
        logChunk: data.log_chunk
    });

    // Job detail panel now subscribes to custom events directly
    // See job-detail-panel.ts subscribeToEvents()
}
