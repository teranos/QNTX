/**
 * Pulse Panel - Scheduled Jobs Dashboard
 *
 * Manifests as a glyph with 'panel' manifestationType — slides in from
 * the opposite edge of the system drawer.
 *
 * Displays all scheduled Pulse jobs:
 * - Lists all scheduled jobs with their intervals
 * - Shows job state (active, paused, stopping, inactive)
 * - Displays next run time and last execution
 * - Provides pause/resume/delete controls
 * - Updates in real-time via WebSocket
 * - Inline execution history (expandable per job)
 * - Force trigger button for immediate execution
 */

import type { ScheduledJobResponse } from './pulse/types';
import { listScheduledJobs } from './pulse/api';
import { PulsePanelState } from './pulse/panel-state';
import * as PanelRenderer from './pulse/panel';
import { attachPanelEventListeners } from './pulse/panel-events';
import { subscribeToExecutionEvents } from './pulse/panel-subscriptions';
import {
    handleForceTrigger,
    handleJobAction,
    handleLoadMore,
    handleRetryExecutions,
    handleProseLocationClick,
    toggleJobExpansion,
    loadExecutionsForJob,
    handleToggleExecution,
    handleToggleChild,
    handleAutoLoadTaskLogs,
    type JobActionContext,
} from './pulse/job-actions';
import { hydrateButtons, registerButton, type HydrateConfig } from './components/button';
import { tooltip } from './components/tooltip.ts';
import type { DaemonStatusMessage } from '../types/websocket';
import type { Glyph } from '@qntx/glyphs';
import { Pulse } from '@generated/sym.js';
import { log, SEG } from './logger.ts';

// Pre-import dynamic modules to avoid chunk-load latency on first render
const systemStatusModule = import('./pulse/system-status.ts');
const activeQueueModule = import('./pulse/active-queue.ts');
const schedulesModule = import('./pulse/schedules.ts');

// Module-level state — persists across panel open/close for instant re-open
let contentElement: HTMLElement | null = null;
let jobs: Map<string, ScheduledJobResponse> = new Map();
let state: PulsePanelState = new PulsePanelState();
let currentDaemonStatus: DaemonStatusMessage | null = null;
let activeQueueCleanupTimer: ReturnType<typeof setTimeout> | null = null;
let tooltipCleanup: (() => void) | null = null;
let cachedActiveQueue: import('./pulse/active-queue.ts').ActiveQueueResult | null = null;

// Eagerly prefetch schedule data so it's ready before panel opens
listScheduledJobs().then(result => {
    result.forEach(job => jobs.set(job.id, job));
}).catch(() => { /* silent — will retry on panel open */ });

function getActionContext(): JobActionContext {
    return {
        get jobs() { return jobs; },
        get state() { return state; },
        render: () => { renderSchedules(); refreshTooltips(); return Promise.resolve(); },
        loadJobs: () => loadJobs(),
    };
}

async function loadJobs(): Promise<void> {
    try {
        const result = await listScheduledJobs();

        jobs.clear();
        result.forEach(job => jobs.set(job.id, job));

        // Clean up orphaned job IDs from expandedJobs
        state.cleanupOrphanedJobs(new Set(jobs.keys()));

        renderSchedules();
        refreshTooltips();
    } catch (error: unknown) {
        log.error(SEG.ERROR, '[Pulse Panel] Failed to load jobs:', error);
        const container = contentElement?.querySelector('#pulse-schedules-content');
        if (container) {
            const err = error instanceof Error ? error : new Error(String(error));
            container.innerHTML = `<div class="panel-error"><div class="panel-error-title">Failed to load jobs</div><div class="panel-error-message">${err.message}</div></div>`;
        }
    }
}

async function render(): Promise<void> {
    await Promise.all([renderSystemStatus(), renderActiveQueue()]);
    renderSchedules();
    refreshTooltips();
}

async function renderSystemStatus(): Promise<void> {
    const container = contentElement?.querySelector('#pulse-system-status-content');
    if (!container) return;

    const { renderSystemStatus: renderStatus } = await systemStatusModule;
    container.innerHTML = renderStatus(currentDaemonStatus);
    attachSystemStatusHandlers();
}

async function renderActiveQueue(): Promise<void> {
    const container = contentElement?.querySelector('#pulse-active-queue-content');
    if (!container) return;

    const { renderActiveQueue: renderQueue, fetchActiveJobs } = await activeQueueModule;

    // Show cached data instantly, then refresh
    if (cachedActiveQueue) {
        container.innerHTML = renderQueue(cachedActiveQueue);
    }

    const result = await fetchActiveJobs();
    cachedActiveQueue = result;
    container.innerHTML = renderQueue(result);

    // Schedule cleanup re-render when recently-finished jobs will expire
    if (activeQueueCleanupTimer) {
        clearTimeout(activeQueueCleanupTimer);
        activeQueueCleanupTimer = null;
    }
    if (result.hasRecent) {
        activeQueueCleanupTimer = setTimeout(() => {
            activeQueueCleanupTimer = null;
            renderActiveQueue();
        }, 8000);
    }
}

function renderSchedules(): void {
    const container = contentElement?.querySelector('#pulse-schedules-content');
    if (!container) return;

    schedulesModule.then(({ renderEmptyState, renderJobTable }) => {
        if (jobs.size === 0) {
            container.innerHTML = renderEmptyState();
            return;
        }

        const sortedJobs = Array.from(jobs.values())
            .sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime());

        container.innerHTML = renderJobTable(sortedJobs, state);

        // Hydrate buttons for each job
        hydrateJobButtons(container as HTMLElement);

        // Fetch executions for pre-expanded jobs that don't have cached data
        const ctx = getActionContext();
        for (const jobId of state.expandedJobs) {
            if (!state.jobExecutions.has(jobId) && !state.loadingExecutions.has(jobId) && jobs.has(jobId)) {
                loadExecutionsForJob(jobId, ctx);
            }
        }
    });
}

function hydrateJobButtons(container: HTMLElement): void {
    const ctx = getActionContext();
    const config: HydrateConfig = {};

    for (const job of jobs.values()) {
        const isActive = job.state === 'active';

        config[`force-trigger-${job.id}`] = {
            label: 'Force Trigger',
            onClick: async () => {
                await handleForceTrigger(job.id, ctx);
            },
            variant: 'warning',
            size: 'small',
            confirmation: {
                label: 'Confirm Trigger',
                timeout: 5000
            }
        };

        config[`toggle-state-${job.id}`] = {
            label: isActive ? 'Pause' : 'Resume',
            onClick: async () => {
                await handleJobAction(job.id, isActive ? 'pause' : 'resume', ctx);
            },
            variant: isActive ? 'secondary' : 'primary',
            size: 'small'
        };

        config[`delete-${job.id}`] = {
            label: 'Delete',
            onClick: async () => {
                await handleJobAction(job.id, 'delete', ctx);
            },
            variant: 'danger',
            size: 'small',
            confirmation: {
                label: 'Confirm Delete',
                timeout: 5000
            }
        };
    }

    const buttons = hydrateButtons(container, config);
    for (const [buttonId, button] of Object.entries(buttons)) {
        registerButton(buttonId, button);
    }
}

function attachSystemStatusHandlers(): void {
    const container = contentElement?.querySelector('#pulse-system-status-content');
    if (!container) return;

    const daemonBtn = container.querySelector('[data-action="start-daemon"], [data-action="stop-daemon"]') as HTMLButtonElement;
    if (daemonBtn) {
        daemonBtn.addEventListener('click', async (e) => {
            e.preventDefault();
            const action = daemonBtn.dataset.action;
            if (action) {
                await handleSystemStatusAction(action);
            }
        });
    }

    const budgetBtn = container.querySelector('[data-action="edit-budget"]') as HTMLButtonElement;
    if (budgetBtn) {
        budgetBtn.addEventListener('click', async (e) => {
            e.preventDefault();
            await handleSystemStatusAction('edit-budget');
        });
    }

    container.addEventListener('daemon-confirm-reset', async () => {
        await renderSystemStatus();
    });
}

async function handleSystemStatusAction(action: string): Promise<void> {
    const { handleSystemStatusAction: handle } = await import('./pulse/system-status.ts');
    const executed = await handle(action);

    if (!executed) {
        await renderSystemStatus();
    }
}

function refreshTooltips(): void {
    if (!contentElement) return;
    if (tooltipCleanup) {
        tooltipCleanup();
    }
    tooltipCleanup = tooltip.attach(contentElement, '.has-tooltip');
}

/**
 * Update daemon status from WebSocket
 * Called via custom event dispatched by websocket-handlers/daemon-status.ts
 */
function handleDaemonStatusUpdate(e: Event): void {
    const detail = (e as CustomEvent<DaemonStatusMessage>).detail;
    currentDaemonStatus = detail;

    if (contentElement?.isConnected) {
        renderSystemStatus();
    }
}

/**
 * Create a Glyph definition for the pulse panel
 */
export function createPulseGlyph(): Glyph {
    return {
        id: 'pulse-glyph',
        title: `${Pulse} Pulse`,
        manifestationType: 'panel',
        renderContent: () => {
            const content = document.createElement('div');
            contentElement = content;

            // Render template
            content.innerHTML = PanelRenderer.renderPanelTemplate();

            // Attach delegated event listeners
            const ctx = getActionContext();
            const cleanupEvents = attachPanelEventListeners(content, {
                onToggleExpansion: (jobId) => toggleJobExpansion(jobId, ctx),
                onLoadMore: (jobId) => handleLoadMore(jobId, ctx),
                onRetryExecutions: (jobId) => handleRetryExecutions(jobId, ctx),
                onProseLocation: (docId) => handleProseLocationClick(docId),
                onToggleExecution: (executionId, asyncJobId) => handleToggleExecution(executionId, asyncJobId, ctx),
                onToggleChild: (childId) => handleToggleChild(childId, ctx),
                onAutoLoadTaskLogs: (jobId, taskId) => handleAutoLoadTaskLogs(jobId, taskId, ctx),
            });

            // Subscribe to real-time execution events
            const unsubscribers = subscribeToExecutionEvents({
                jobs,
                state,
                isVisible: () => contentElement?.isConnected ?? false,
                render: () => render(),
            });

            // Listen for daemon status updates
            document.addEventListener('pulse-daemon-status', handleDaemonStatusUpdate);

            // Cleanup when panel is closed (element disconnected from DOM)
            const cleanupInterval = setInterval(() => {
                if (!contentElement?.isConnected) {
                    clearInterval(cleanupInterval);
                    cleanupEvents();
                    unsubscribers.forEach(unsub => unsub());
                    document.removeEventListener('pulse-daemon-status', handleDaemonStatusUpdate);
                    if (activeQueueCleanupTimer) {
                        clearTimeout(activeQueueCleanupTimer);
                        activeQueueCleanupTimer = null;
                    }
                    if (tooltipCleanup) {
                        tooltipCleanup();
                        tooltipCleanup = null;
                    }
                    contentElement = null;
                    return;
                }
            }, 2000);

            // Show cached data instantly (system status is always from WebSocket cache)
            renderSystemStatus();

            // If we have cached data, render immediately then refresh in background
            if (jobs.size > 0) {
                renderSchedules();
            }
            if (cachedActiveQueue) {
                // Already rendered by renderActiveQueue's cache path
            }

            // Refresh all sections in background
            renderActiveQueue();
            loadJobs();

            return content;
        }
    };
}
