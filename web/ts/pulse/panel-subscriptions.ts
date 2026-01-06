/**
 * Pulse Panel Event Subscriptions
 *
 * Handles real-time WebSocket event subscriptions for the pulse panel.
 * Updates job state and execution lists in response to execution events.
 */

import type { ScheduledJobResponse } from './types';
import type { PulsePanelState } from './panel-state';
import {
    onExecutionStarted,
    onExecutionCompleted,
    onExecutionFailed,
    unixToISO,
} from './events';

/**
 * Context for subscription handlers
 */
export interface SubscriptionContext {
    jobs: Map<string, ScheduledJobResponse>;
    state: PulsePanelState;
    isVisible: () => boolean;
    render: () => Promise<void>;
}

/**
 * Subscribe to all pulse execution events
 * Returns an array of unsubscribe functions
 */
export function subscribeToExecutionEvents(ctx: SubscriptionContext): Array<() => void> {
    const unsubscribers: Array<() => void> = [];

    // Execution started - update last run time and add to inline list
    unsubscribers.push(
        onExecutionStarted((detail) => {
            if (!ctx.isVisible()) return;

            const job = ctx.jobs.get(detail.scheduledJobId);
            const timestamp = unixToISO(detail.timestamp);
            if (job) {
                job.last_run_at = timestamp;
            }

            // Add to inline execution list if job is expanded
            if (ctx.state.isExpanded(detail.scheduledJobId)) {
                const executions = ctx.state.getExecutions(detail.scheduledJobId) || [];
                executions.unshift({
                    id: detail.executionId,
                    scheduled_job_id: detail.scheduledJobId,
                    status: 'running',
                    started_at: timestamp,
                    created_at: timestamp,
                    updated_at: timestamp,
                } as any);
                ctx.state.setExecutions(detail.scheduledJobId, executions);
            }

            ctx.render();
        })
    );

    // Execution completed - update execution status
    unsubscribers.push(
        onExecutionCompleted((detail) => {
            if (!ctx.isVisible()) return;

            const job = ctx.jobs.get(detail.scheduledJobId);
            const timestamp = unixToISO(detail.timestamp);
            if (job) {
                job.last_run_at = timestamp;
            }

            // Update inline execution if expanded
            for (const [_jobId, executions] of ctx.state.jobExecutions.entries()) {
                const execution = executions.find(e => e.id === detail.executionId);
                if (execution) {
                    Object.assign(execution, {
                        status: 'completed',
                        async_job_id: detail.asyncJobId,
                        result_summary: detail.resultSummary,
                        duration_ms: detail.durationMs,
                        completed_at: timestamp,
                    });
                    break;
                }
            }

            ctx.render();
        })
    );

    // Execution failed - update execution status
    unsubscribers.push(
        onExecutionFailed((detail) => {
            if (!ctx.isVisible()) return;

            const job = ctx.jobs.get(detail.scheduledJobId);
            const timestamp = unixToISO(detail.timestamp);
            if (job) {
                job.last_run_at = timestamp;
            }

            // Update inline execution if expanded
            for (const [_jobId, executions] of ctx.state.jobExecutions.entries()) {
                const execution = executions.find(e => e.id === detail.executionId);
                if (execution) {
                    Object.assign(execution, {
                        status: 'failed',
                        error_message: detail.errorMessage,
                        duration_ms: detail.durationMs,
                        completed_at: timestamp,
                    });
                    break;
                }
            }

            ctx.render();
        })
    );

    return unsubscribers;
}
