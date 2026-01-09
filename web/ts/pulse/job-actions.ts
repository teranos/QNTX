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

/**
 * Two-click confirmation state for job actions
 */
interface ConfirmationState {
    needsConfirmation: boolean;
    timeout: number | null;
}

// Track confirmation state per job per action
const forceTriggerConfirmation = new Map<string, ConfirmationState>();
const deleteConfirmation = new Map<string, ConfirmationState>();

/**
 * Reset confirmation state for a job action
 */
function resetConfirmation(
    confirmMap: Map<string, ConfirmationState>,
    jobId: string
): void {
    const state = confirmMap.get(jobId);
    if (state?.timeout) {
        clearTimeout(state.timeout);
    }
    confirmMap.delete(jobId);
}

/**
 * Update button visual state for confirmation
 */
function updateButtonConfirmState(
    button: HTMLElement,
    isConfirming: boolean,
    originalText: string,
    confirmText: string
): void {
    if (isConfirming) {
        button.classList.add('pulse-btn-confirming');
        button.textContent = confirmText;
        button.setAttribute('data-original-text', originalText);
    } else {
        button.classList.remove('pulse-btn-confirming');
        const original = button.getAttribute('data-original-text') || originalText;
        button.textContent = original;
        button.removeAttribute('data-original-text');
    }
}

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
 * Uses two-click confirmation pattern
 */
export async function handleForceTrigger(
    jobId: string,
    ctx: JobActionContext,
    buttonElement?: HTMLElement
): Promise<void> {
    const job = ctx.jobs.get(jobId);
    if (!job) return;

    // Get the button element if not passed
    const button = buttonElement || document.querySelector(
        `.pulse-job-card[data-job-id="${jobId}"] [data-action="force-trigger"]`
    ) as HTMLElement | null;

    // Check if we're in confirmation state
    const state = forceTriggerConfirmation.get(jobId);

    if (!state?.needsConfirmation) {
        // First click: enter confirmation state
        forceTriggerConfirmation.set(jobId, {
            needsConfirmation: true,
            timeout: window.setTimeout(() => {
                resetConfirmation(forceTriggerConfirmation, jobId);
                if (button) {
                    updateButtonConfirmState(button, false, 'Force Trigger', 'Confirm');
                }
            }, 5000)
        });

        if (button) {
            updateButtonConfirmState(button, true, 'Force Trigger', 'Confirm Trigger');
        }

        return;
    }

    // Second click: execute action
    resetConfirmation(forceTriggerConfirmation, jobId);
    if (button) {
        updateButtonConfirmState(button, false, 'Force Trigger', 'Confirm');
    }

    try {
        log.debug(SEG.PULSE, 'Force triggering job:', job.ats_code);

        await forceTriggerJob(job.ats_code);

        if (!ctx.state.expandedJobs.has(jobId)) {
            ctx.state.expandedJobs.add(jobId);
            ctx.state.saveToLocalStorage();
        }

        await loadExecutionsForJob(jobId, ctx);

        toast.success('Force trigger started - check execution history below');
    } catch (error) {
        handleError(error, 'Force trigger failed', { context: SEG.PULSE, showBuildInfo: true });
    }
}

/**
 * Handle job lifecycle actions (pause, resume, delete)
 * Delete uses two-click confirmation pattern
 */
export async function handleJobAction(
    jobId: string,
    action: string,
    ctx: JobActionContext,
    buttonElement?: HTMLElement
): Promise<void> {
    // Handle delete with two-click confirmation
    if (action === 'delete') {
        const button = buttonElement || document.querySelector(
            `.pulse-job-card[data-job-id="${jobId}"] [data-action="delete"]`
        ) as HTMLElement | null;

        const state = deleteConfirmation.get(jobId);

        if (!state?.needsConfirmation) {
            // First click: enter confirmation state
            deleteConfirmation.set(jobId, {
                needsConfirmation: true,
                timeout: window.setTimeout(() => {
                    resetConfirmation(deleteConfirmation, jobId);
                    if (button) {
                        updateButtonConfirmState(button, false, 'Delete', 'Confirm Delete');
                    }
                }, 5000)
            });

            if (button) {
                updateButtonConfirmState(button, true, 'Delete', 'Confirm Delete');
            }

            return;
        }

        // Second click: execute delete
        resetConfirmation(deleteConfirmation, jobId);
        if (button) {
            updateButtonConfirmState(button, false, 'Delete', 'Confirm Delete');
        }
    }

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
    } catch (error) {
        handleError(error, `Failed to ${action} job`, { context: SEG.PULSE });
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
