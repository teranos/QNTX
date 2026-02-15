/**
 * Pulse Panel Job Actions
 *
 * Handles all job-related actions: force trigger, pause, resume, delete,
 * expansion toggle, execution loading, and navigation.
 *
 * Uses two-click confirmation pattern for destructive actions:
 * - Force Trigger: Bypasses scheduling, needs confirmation
 * - Delete: Permanently removes job, needs confirmation
 */

import type { ScheduledJobResponse } from './types';
import { pauseScheduledJob, resumeScheduledJob, deleteScheduledJob, forceTriggerJob } from './api';
import { toast } from '../toast';
import { listExecutions } from './execution-api';
import type { PulsePanelState } from './panel-state';
import { handleError, SEG } from '../error-handler';
import { log } from '../logger';
import { Button } from '../components/button';

// Note: Two-click confirmation is now handled by the Button component
// The old manual confirmation state tracking has been removed

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
 * Note: Confirmation is now handled by the Button component
 */
export async function handleForceTrigger(
    jobId: string,
    ctx: JobActionContext
): Promise<void> {
    const job = ctx.jobs.get(jobId);
    if (!job) return;

    try {
        log.debug(SEG.PULSE, 'Force triggering job:', job.ats_code || job.handler_name);

        await forceTriggerJob(job.ats_code, job.handler_name);

        if (!ctx.state.expandedJobs.has(jobId)) {
            ctx.state.expandedJobs.add(jobId);
            ctx.state.saveToLocalStorage();
        }

        await loadExecutionsForJob(jobId, ctx);

        toast.success('Force trigger started - check execution history below');
    } catch (error: unknown) {
        handleError(error, 'Force trigger failed', { context: SEG.PULSE, showBuildInfo: true });
        throw error; // Re-throw so Button component can show error state
    }
}

/**
 * Handle job lifecycle actions (pause, resume, delete)
 * Note: Confirmation for delete is now handled by the Button component
 */
export async function handleJobAction(
    jobId: string,
    action: string,
    ctx: JobActionContext
): Promise<void> {
    try {
        switch (action) {
            case 'pause':
                await pauseScheduledJob(jobId);
                break;
            case 'resume':
                await resumeScheduledJob(jobId);
                break;
            case 'delete':
                await deleteScheduledJob(jobId);
                break;
        }

        await ctx.loadJobs();
    } catch (error: unknown) {
        handleError(error, `Failed to ${action} job`, { context: SEG.PULSE });
        throw error; // Re-throw so Button component can show error state
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
    } catch (error: unknown) {
        const err = handleError(error, 'Failed to load execution history', { context: SEG.PULSE });
        ctx.state.executionErrors.set(jobId, err.message);
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
    log.debug(SEG.PULSE, 'Opening prose document:', docId);

    const { showProseDocument } = await import('../prose/panel.js');
    await showProseDocument(docId);
}

// ============================================================================
// Button Component Factory Functions
// Use these when creating job action buttons programmatically
// ============================================================================

/**
 * Create a Force Trigger button using the Button component
 * Includes two-stage confirmation
 */
export function createForceTriggerButton(
    job: ScheduledJobResponse,
    ctx: JobActionContext
): Button {
    return new Button({
        label: 'Force Trigger',
        onClick: async () => {
            log.debug(SEG.PULSE, 'Force triggering job:', job.ats_code || job.handler_name);
            await forceTriggerJob(job.ats_code, job.handler_name);

            if (!ctx.state.expandedJobs.has(job.id)) {
                ctx.state.expandedJobs.add(job.id);
                ctx.state.saveToLocalStorage();
            }

            await loadExecutionsForJob(job.id, ctx);
            toast.success('Force trigger started - check execution history below');
        },
        variant: 'warning',
        size: 'small',
        confirmation: {
            label: 'Confirm Trigger',
            timeout: 5000
        }
    });
}

/**
 * Create a Delete button using the Button component
 * Includes two-stage confirmation
 */
export function createDeleteButton(
    jobId: string,
    ctx: JobActionContext
): Button {
    return new Button({
        label: 'Delete',
        onClick: async () => {
            await deleteScheduledJob(jobId);
            await ctx.loadJobs();
        },
        variant: 'danger',
        size: 'small',
        confirmation: {
            label: 'Confirm Delete',
            timeout: 5000
        }
    });
}

/**
 * Create a Pause button using the Button component
 */
export function createPauseButton(
    jobId: string,
    ctx: JobActionContext
): Button {
    return new Button({
        label: 'Pause',
        onClick: async () => {
            await pauseScheduledJob(jobId);
            await ctx.loadJobs();
        },
        variant: 'secondary',
        size: 'small'
    });
}

/**
 * Create a Resume button using the Button component
 */
export function createResumeButton(
    jobId: string,
    ctx: JobActionContext
): Button {
    return new Button({
        label: 'Resume',
        onClick: async () => {
            await resumeScheduledJob(jobId);
            await ctx.loadJobs();
        },
        variant: 'primary',
        size: 'small'
    });
}
