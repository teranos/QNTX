/**
 * Pulse Panel Job Actions
 *
 * Handles all job-related actions: force trigger, pause, resume, delete,
 * expansion toggle, execution loading, and navigation.
 */

import { debugLog } from '../debug';
import type { ScheduledJobResponse } from './types';
import { pauseScheduledJob, resumeScheduledJob, deleteScheduledJob, forceTriggerJob } from './api';
import { formatInterval } from './types';
import { toast } from '../toast';
import { listExecutions } from './execution-api';
import type { PulsePanelState } from './panel-state';

/**
 * Context passed to job action handlers
 */
export interface JobActionContext {
    jobs: Map<string, ScheduledJobResponse>;
    state: PulsePanelState;
    render: () => Promise<void>;
    loadJobs: () => Promise<void>;
}

/**
 * Force trigger a job for immediate execution
 */
export async function handleForceTrigger(
    jobId: string,
    ctx: JobActionContext
): Promise<void> {
    const job = ctx.jobs.get(jobId);
    if (!job) return;

    try {
        debugLog('[Pulse Panel] Force triggering job:', job.ats_code);

        await forceTriggerJob(job.ats_code);

        if (!ctx.state.expandedJobs.has(jobId)) {
            ctx.state.expandedJobs.add(jobId);
            ctx.state.saveToLocalStorage();
        }

        await loadExecutionsForJob(jobId, ctx);

        toast.success('Force trigger started - check execution history below');
    } catch (error) {
        console.error('[Pulse Panel] Force trigger failed:', error);
        toast.error(`Force trigger failed: ${(error as Error).message}`, true);
    }
}

/**
 * Handle job lifecycle actions (pause, resume, delete)
 */
export async function handleJobAction(
    jobId: string,
    action: string,
    ctx: JobActionContext
): Promise<void> {
    const job = ctx.jobs.get(jobId);

    try {
        switch (action) {
            case 'pause':
                await pauseScheduledJob(jobId);
                break;
            case 'resume':
                await resumeScheduledJob(jobId);
                break;
            case 'delete':
                if (!confirm('Delete this scheduled job?')) return;
                await deleteScheduledJob(jobId);
                break;
        }

        await ctx.loadJobs();
    } catch (error) {
        console.error(`[Pulse Panel] Failed to ${action} job:`, error);

        let errorMsg = `Failed to ${action} job: ${(error as Error).message}`;

        if (job) {
            errorMsg += `\n\nATS Code:\n${job.ats_code}`;
            errorMsg += `\nInterval: ${formatInterval(job.interval_seconds ?? 0)}`;
            if (job.created_from_doc) {
                errorMsg += `\nDocument: ${job.created_from_doc}`;
            }
        }

        toast.error(errorMsg);
    }
}

/**
 * Toggle job expansion (show/hide execution history)
 */
export async function toggleJobExpansion(
    jobId: string,
    ctx: JobActionContext
): Promise<void> {
    if (ctx.state.expandedJobs.has(jobId)) {
        ctx.state.expandedJobs.delete(jobId);
        ctx.state.saveToLocalStorage();
        await ctx.render();
    } else {
        ctx.state.expandedJobs.add(jobId);
        ctx.state.saveToLocalStorage();
        await ctx.render();

        if (!ctx.state.jobExecutions.has(jobId) && !ctx.state.loadingExecutions.has(jobId)) {
            await loadExecutionsForJob(jobId, ctx);
        }
    }
}

/**
 * Load execution history for a job
 */
export async function loadExecutionsForJob(
    jobId: string,
    ctx: JobActionContext
): Promise<void> {
    ctx.state.loadingExecutions.add(jobId);
    ctx.state.executionErrors.delete(jobId);
    await ctx.render();

    try {
        const response = await listExecutions(jobId, { limit: 20, offset: 0 });
        ctx.state.jobExecutions.set(jobId, response.executions);
        ctx.state.executionErrors.delete(jobId);
    } catch (error) {
        console.error('[Pulse Panel] Failed to load executions:', error);
        const errorMessage = (error as Error).message || 'Unknown error';
        ctx.state.executionErrors.set(jobId, errorMessage);
        toast.error(`Failed to load execution history: ${errorMessage}`);
    } finally {
        ctx.state.loadingExecutions.delete(jobId);
        await ctx.render();
    }
}

/**
 * Load more executions for a job
 */
export async function handleLoadMore(
    jobId: string,
    ctx: JobActionContext
): Promise<void> {
    const currentLimit = ctx.state.executionLimits.get(jobId) || 5;
    ctx.state.executionLimits.set(jobId, currentLimit + 10);
    await ctx.render();
}

/**
 * Retry loading executions after an error
 */
export async function handleRetryExecutions(
    jobId: string,
    ctx: JobActionContext
): Promise<void> {
    ctx.state.executionErrors.delete(jobId);
    await loadExecutionsForJob(jobId, ctx);
}

/**
 * Open job detail panel
 */
export async function handleViewDetailed(
    jobId: string,
    ctx: JobActionContext
): Promise<void> {
    const job = ctx.jobs.get(jobId);
    if (!job) return;

    const { showJobDetail } = await import('./job-detail-panel.js');
    showJobDetail(job);
}

/**
 * Open prose document that created this job
 */
export async function handleProseLocationClick(docId: string): Promise<void> {
    debugLog('[Pulse Panel] Opening prose document:', docId);

    const { showProseDocument } = await import('../prose/panel.js');
    await showProseDocument(docId);
}
