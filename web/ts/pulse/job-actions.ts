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
import { listExecutions, getJobStages, getJobChildren, getTaskLogsForJob } from './execution-api';
import type { PulsePanelState } from './panel-state';
import { handleError, SEG } from '../error-handler';
import { log } from '../logger';

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
 * Toggle execution expansion — load stages/children on first expand
 */
export async function handleToggleExecution(
    executionId: string,
    asyncJobId: string,
    ctx: JobActionContext
): Promise<void> {
    const { state } = ctx;

    if (state.expandedExecutions.has(executionId)) {
        state.expandedExecutions.delete(executionId);
        await ctx.render();
        return;
    }

    state.expandedExecutions.add(executionId);
    await ctx.render();

    if (asyncJobId && !state.executionStages.has(executionId)) {
        try {
            const stages = await getJobStages(asyncJobId);
            state.executionStages.set(executionId, stages);

            if (stages.stages.length === 0) {
                const children = await getJobChildren(asyncJobId);
                state.executionChildren.set(executionId, children);
            }
        } catch (error: unknown) {
            handleError(error, 'Failed to load execution detail', { context: SEG.PULSE, silent: true });
            state.executionStages.set(executionId, { job_id: asyncJobId, stages: [] });
        }
        await ctx.render();
    }
}

/**
 * Toggle child job expansion — load stages on first expand
 */
export async function handleToggleChild(
    childId: string,
    ctx: JobActionContext
): Promise<void> {
    const { state } = ctx;

    if (state.expandedChildren.has(childId)) {
        state.expandedChildren.delete(childId);
        await ctx.render();
        return;
    }

    state.expandedChildren.add(childId);
    await ctx.render();

    if (!state.childStages.has(childId)) {
        try {
            const stages = await getJobStages(childId);
            state.childStages.set(childId, stages);
        } catch (error: unknown) {
            handleError(error, 'Failed to load child job stages', { context: SEG.PULSE, silent: true });
            state.childStages.set(childId, { job_id: childId, stages: [] });
        }
        await ctx.render();
    }
}

/**
 * Auto-load task logs when a task loading placeholder appears
 */
export async function handleAutoLoadTaskLogs(
    jobId: string,
    taskId: string,
    ctx: JobActionContext
): Promise<void> {
    const taskKey = `${jobId}:${taskId}`;
    const { state } = ctx;

    if (state.taskLogs.has(taskKey) || state.loadingTasks.has(taskKey)) return;

    state.loadingTasks.add(taskKey);

    try {
        const logs = await getTaskLogsForJob(jobId, taskId);
        state.taskLogs.set(taskKey, logs);
    } catch (error: unknown) {
        handleError(error, 'Failed to load task logs', { context: SEG.PULSE, silent: true });
        state.taskLogs.set(taskKey, { task_id: taskId, logs: [] });
    } finally {
        state.loadingTasks.delete(taskKey);
    }

    await ctx.render();
}

/**
 * Open prose document that created this job.
 * Prose panel removed — this is a no-op until document viewing migrates to a glyph.
 */
export async function handleProseLocationClick(docId: string): Promise<void> {
    log.debug(SEG.PULSE, 'Prose panel removed, ignoring document open:', docId);
}

