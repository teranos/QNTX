/**
 * Real-time WebSocket handlers for Pulse execution events
 *
 * Handles:
 * - Execution started notifications
 * - Execution failed notifications (with toast)
 * - Execution completed notifications
 * - Live log streaming
 *
 * Updates both:
 * - Pulse panel inline execution view (when job is expanded)
 * - Job detail panel full history (when viewing specific job)
 *
 * TODO: Future real-time improvements:
 * - Add execution progress updates (percentage completion)
 * - Add live resource usage metrics (CPU, memory for running jobs)
 * - Add execution cancellation via WebSocket
 * - Add batch execution status updates (reduce message frequency)
 */

import { debugLog } from '../debug';
import {
    PulseExecutionStartedMessage,
    PulseExecutionFailedMessage,
    PulseExecutionCompletedMessage,
    PulseExecutionLogStreamMessage
} from '../../types/websocket';
import { toast } from '../toast';
import type { ScheduledJob } from './types';
import type { PulseExecution } from './execution-types';
import type { PulsePanelState } from './panel-state';

// ========================================================================
// Panel update functions (extracted from pulse-panel.ts)
// ========================================================================

/**
 * Update last run timestamp for a job
 * Returns true if job was found and updated
 */
export function updatePanelJobLastRun(
    jobs: Map<string, ScheduledJob>,
    jobId: string,
    lastRunAt: string
): boolean {
    const job = jobs.get(jobId);
    if (!job) return false;

    // Update job data
    job.last_run_at = lastRunAt;
    return true;
}

/**
 * Add or update execution in panel state
 * Returns true if execution was added/updated
 */
export function addExecutionToPanel(
    state: PulsePanelState,
    execution: Partial<PulseExecution>
): boolean {
    if (!execution.scheduled_job_id) return false;

    const jobId = execution.scheduled_job_id;

    // Only add if this job is expanded
    if (!state.isExpanded(jobId)) return false;

    // Get existing executions for this job
    const executions = state.getExecutions(jobId) || [];

    // Add new execution at the start
    executions.unshift(execution as PulseExecution);
    state.setExecutions(jobId, executions);

    return true;
}

/**
 * Update execution status/data in panel state
 * Returns true if execution was found and updated
 */
export function updatePanelExecutionStatus(
    state: PulsePanelState,
    executionId: string,
    updates: Partial<PulseExecution>
): boolean {
    // Find which job contains this execution
    for (const [jobId, executions] of state.jobExecutions.entries()) {
        const execution = executions.find(e => e.id === executionId);
        if (execution) {
            // Update execution fields
            Object.assign(execution, updates);
            return true;
        }
    }
    return false;
}

// ========================================================================
// WebSocket event handlers
// ========================================================================

/**
 * Handle execution started notification
 * Updates job card "last run" time if Pulse panel is visible
 * Adds execution to detail panel list if that job's panel is open
 */
export function handlePulseExecutionStarted(data: PulseExecutionStartedMessage): void {
    debugLog('[Pulse Realtime] Execution started:', {
        job_id: data.scheduled_job_id,
        execution_id: data.execution_id,
        ats_code: data.ats_code
    });

    // Update Pulse panel job card if it exists
    const pulsePanel = (window as any).pulsePanel;
    if (pulsePanel && pulsePanel.isVisible) {
        // Update last run timestamp
        if (updatePanelJobLastRun(pulsePanel.jobs, data.scheduled_job_id, new Date(data.timestamp * 1000).toISOString())) {
            pulsePanel.render();
        }

        // Also add to inline execution list if job is expanded
        if (addExecutionToPanel(pulsePanel.state, {
            id: data.execution_id,
            scheduled_job_id: data.scheduled_job_id,
            status: 'running',
            started_at: new Date(data.timestamp * 1000).toISOString(),
            created_at: new Date(data.timestamp * 1000).toISOString(),
            updated_at: new Date(data.timestamp * 1000).toISOString(),
        })) {
            pulsePanel.render();
        }
    }

    // Update job detail panel if viewing this job
    const jobDetailPanel = (window as any).jobDetailPanel;
    if (jobDetailPanel && jobDetailPanel.isShowingJob(data.scheduled_job_id)) {
        jobDetailPanel.addStartedExecution({
            id: data.execution_id,
            scheduled_job_id: data.scheduled_job_id,
            status: 'running',
            started_at: new Date(data.timestamp * 1000).toISOString(),
            created_at: new Date(data.timestamp * 1000).toISOString(),
            updated_at: new Date(data.timestamp * 1000).toISOString(),
        });
    }
}

/**
 * Handle execution failed notification
 * Updates UI and ALWAYS shows failure toast
 */
export function handlePulseExecutionFailed(data: PulseExecutionFailedMessage): void {
    debugLog('[Pulse Realtime] Execution failed:', {
        job_id: data.scheduled_job_id,
        execution_id: data.execution_id,
        error: data.error_message,
        duration_ms: data.duration_ms
    });

    // Update Pulse panel job card
    const pulsePanel = (window as any).pulsePanel;
    if (pulsePanel && pulsePanel.isVisible) {
        let needsRender = false;

        // Update last run timestamp
        if (updatePanelJobLastRun(pulsePanel.jobs, data.scheduled_job_id, new Date(data.timestamp * 1000).toISOString())) {
            needsRender = true;
        }

        // Also update inline execution list if job is expanded
        if (updatePanelExecutionStatus(pulsePanel.state, data.execution_id, {
            status: 'failed',
            error_message: data.error_message,
            duration_ms: data.duration_ms,
            completed_at: new Date(data.timestamp * 1000).toISOString(),
        })) {
            needsRender = true;
        }

        if (needsRender) {
            pulsePanel.render();
        }
    }

    // Update job detail panel if viewing this job
    const jobDetailPanel = (window as any).jobDetailPanel;
    if (jobDetailPanel && jobDetailPanel.isShowingJob(data.scheduled_job_id)) {
        jobDetailPanel.updateExecutionStatus(data.execution_id, {
            status: 'failed',
            error_message: data.error_message,
            duration_ms: data.duration_ms,
            completed_at: new Date(data.timestamp * 1000).toISOString(),
        });
    }

    // ALWAYS show failure toast
    toast.error(`Pulse job failed: ${data.ats_code}\n\nError: ${data.error_message}`);
}

/**
 * Handle execution completed notification
 * Updates job card and detail panel (no toast for success)
 */
export function handlePulseExecutionCompleted(data: PulseExecutionCompletedMessage): void {
    debugLog('[Pulse Realtime] Execution completed:', {
        job_id: data.scheduled_job_id,
        execution_id: data.execution_id,
        async_job_id: data.async_job_id,
        duration_ms: data.duration_ms
    });

    // Update Pulse panel job card
    const pulsePanel = (window as any).pulsePanel;
    if (pulsePanel && pulsePanel.isVisible) {
        let needsRender = false;

        // Update last run timestamp
        if (updatePanelJobLastRun(pulsePanel.jobs, data.scheduled_job_id, new Date(data.timestamp * 1000).toISOString())) {
            needsRender = true;
        }

        // Also update inline execution list if job is expanded
        if (updatePanelExecutionStatus(pulsePanel.state, data.execution_id, {
            status: 'completed',
            async_job_id: data.async_job_id,
            result_summary: data.result_summary,
            duration_ms: data.duration_ms,
            completed_at: new Date(data.timestamp * 1000).toISOString(),
        })) {
            needsRender = true;
        }

        if (needsRender) {
            pulsePanel.render();
        }
    }

    // Update job detail panel if viewing this job
    const jobDetailPanel = (window as any).jobDetailPanel;
    if (jobDetailPanel && jobDetailPanel.isShowingJob(data.scheduled_job_id)) {
        jobDetailPanel.updateExecutionStatus(data.execution_id, {
            status: 'completed',
            async_job_id: data.async_job_id,
            result_summary: data.result_summary,
            duration_ms: data.duration_ms,
            completed_at: new Date(data.timestamp * 1000).toISOString(),
        });
    }
}

/**
 * Handle live log streaming
 * Only appends logs if detail panel is viewing this specific job
 */
export function handlePulseExecutionLogStream(data: PulseExecutionLogStreamMessage): void {
    debugLog('[Pulse Realtime] Log chunk received:', {
        job_id: data.scheduled_job_id,
        execution_id: data.execution_id,
        chunk_length: data.log_chunk.length
    });

    // Only stream logs if detail panel is viewing this job
    const jobDetailPanel = (window as any).jobDetailPanel;
    if (jobDetailPanel && jobDetailPanel.isShowingJob(data.scheduled_job_id)) {
        jobDetailPanel.appendExecutionLog(data.execution_id, data.log_chunk);
    }
}
