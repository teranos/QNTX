/**
 * Pulse Panel - Scheduled Jobs Dashboard
 *
 * Displays all scheduled Pulse jobs when clicking pulse (꩜) in the symbol palette:
 * - Lists all scheduled jobs with their intervals
 * - Shows job state (active, paused, stopping, inactive)
 * - Displays next run time and last execution
 * - Provides pause/resume/delete controls
 * - Updates in real-time via WebSocket
 * - Inline execution history (expandable per job)
 * - Force trigger button for immediate execution
 *
 * TODO: Future improvements for inline execution view:
 * - Add full log viewing capability directly in execution cards (currently requires "View detailed history" link)
 * - Add search/filtering for executions (by status, date range, etc.)
 * - Consider infinite scroll instead of "Load N more" pagination
 * - Add execution progress indicators for running jobs
 * - Add bulk actions (cancel all running, retry failed, etc.)
 */

import { BasePanel } from './base-panel.ts';
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
    handleViewDetailed,
    handleProseLocationClick,
    toggleJobExpansion,
    type JobActionContext,
} from './pulse/job-actions';
import { hydrateButtons, registerButton, type HydrateConfig } from './components/button';
import type { DaemonStatusMessage } from '../types/websocket';
import { log, SEG } from './logger.ts';

// Global daemon status (updated via WebSocket)
let currentDaemonStatus: DaemonStatusMessage | null = null;

// Global pulse panel instance
let pulsePanelInstance: PulsePanel | null = null;

class PulsePanel extends BasePanel {
    private jobs: Map<string, ScheduledJobResponse> = new Map();
    private state: PulsePanelState;
    private unsubscribers: Array<() => void> = [];

    constructor() {
        super({
            id: 'pulse-panel',
            classes: ['panel-slide-left', 'pulse-panel'],
            useOverlay: true,
            closeOnEscape: true
        });

        this.state = new PulsePanelState();
        this.subscribeToEvents();
        pulsePanelInstance = this;
    }

    /**
     * Get job action context for handlers
     */
    private getActionContext(): JobActionContext {
        // Use getters so ctx always resolves current values — setupEventListeners()
        // runs during super() before field initializers set this.jobs/this.state.
        const self = this;
        return {
            get jobs() { return self.jobs; },
            get state() { return self.state; },
            render: () => this.render(),
            loadJobs: () => this.loadJobs(),
        };
    }

    /**
     * Subscribe to Pulse execution events for real-time updates
     */
    private subscribeToEvents(): void {
        this.unsubscribers = subscribeToExecutionEvents({
            jobs: this.jobs,
            state: this.state,
            isVisible: () => this.isVisible,
            render: () => this.render(),
        });
    }

    protected getTemplate(): string {
        return PanelRenderer.renderPanelTemplate();
    }

    protected setupEventListeners(): void {
        // Attach panel event listeners once using event delegation
        // Note: Job action buttons are now hydrated Button components
        const ctx = this.getActionContext();
        const cleanup = attachPanelEventListeners(this.panel!, {
            onToggleExpansion: (jobId) => toggleJobExpansion(jobId, ctx),
            onLoadMore: (jobId) => handleLoadMore(jobId, ctx),
            onRetryExecutions: (jobId) => handleRetryExecutions(jobId, ctx),
            onViewDetailed: (jobId) => handleViewDetailed(jobId, ctx),
            onProseLocation: (docId) => handleProseLocationClick(docId)
        });

        // Store cleanup function for onDestroy
        // Note: unsubscribers array is initialized by field initializer after super() returns
        if (!this.unsubscribers) {
            this.unsubscribers = [];
        }
        this.unsubscribers.push(cleanup);
    }

    protected async onShow(): Promise<void> {
        // Don't use base showLoading() — it wipes .panel-content, destroying our section containers.
        // The template already has per-section "Loading..." placeholders.
        await this.loadJobs();
    }

    private async loadJobs(): Promise<void> {
        const content = this.$('.pulse-panel-content');
        if (!content) return;

        try {
            const jobs = await listScheduledJobs();

            this.jobs.clear();
            jobs.forEach(job => this.jobs.set(job.id, job));

            // Clean up orphaned job IDs from expandedJobs
            this.state.cleanupOrphanedJobs(new Set(this.jobs.keys()));

            await this.render();
        } catch (error: unknown) {
            log.error(SEG.ERROR, '[Pulse Panel] Failed to load jobs:', error);
            const err = error instanceof Error ? error : new Error(String(error));
            this.showErrorState(err);
        }
    }

    private async render(): Promise<void> {
        await this.renderSystemStatus();
        await this.renderActiveQueue();
        this.renderSchedules();

        // Refresh tooltips after dynamic content updates
        this.refreshTooltips();
    }

    private async renderSystemStatus(): Promise<void> {
        const container = this.$('#pulse-system-status-content');
        if (!container) return;

        const { renderSystemStatus } = await import('./pulse/system-status.ts');

        container.innerHTML = renderSystemStatus(currentDaemonStatus);
        this.attachSystemStatusHandlers();
    }

    private async renderActiveQueue(): Promise<void> {
        const container = this.$('#pulse-active-queue-content');
        if (!container) return;

        const { renderActiveQueue, fetchActiveJobs } = await import('./pulse/active-queue.ts');
        const activeJobs = await fetchActiveJobs();

        container.innerHTML = renderActiveQueue(activeJobs);
    }

    private renderSchedules(): void {
        const container = this.$('#pulse-schedules-content');
        if (!container) return;

        import('./pulse/schedules.ts').then(({ renderEmptyState, renderJobCard }) => {
            if (this.jobs.size === 0) {
                container.innerHTML = renderEmptyState();
                return;
            }

            const jobsHtml = Array.from(this.jobs.values())
                .sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime())
                .map(job => renderJobCard(job, this.state))
                .join('');

            container.innerHTML = `<div class="panel-list pulse-jobs-list">${jobsHtml}</div>`;

            // Hydrate buttons for each job
            this.hydrateJobButtons(container as HTMLElement);
        });
    }

    /**
     * Hydrate button placeholders with Button component instances
     * Registers buttons for WebSocket-driven state updates
     */
    private hydrateJobButtons(container: HTMLElement): void {
        const ctx = this.getActionContext();

        // Build hydration config for all jobs
        const config: HydrateConfig = {};

        for (const job of this.jobs.values()) {
            const isActive = job.state === 'active';

            // Force Trigger button with confirmation
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

            // Pause/Resume toggle button
            config[`toggle-state-${job.id}`] = {
                label: isActive ? 'Pause' : 'Resume',
                onClick: async () => {
                    await handleJobAction(job.id, isActive ? 'pause' : 'resume', ctx);
                },
                variant: isActive ? 'secondary' : 'primary',
                size: 'small'
            };

            // Delete button with confirmation
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

        // Hydrate all buttons
        const buttons = hydrateButtons(container, config);

        // Register buttons for WebSocket-driven state updates
        for (const [buttonId, button] of Object.entries(buttons)) {
            registerButton(buttonId, button);
        }
    }

    private attachSystemStatusHandlers(): void {
        const container = this.$('#pulse-system-status-content');
        if (!container) return;

        const daemonBtn = container.querySelector('[data-action="start-daemon"], [data-action="stop-daemon"]') as HTMLButtonElement;
        if (daemonBtn) {
            daemonBtn.addEventListener('click', async (e) => {
                e.preventDefault();
                const action = daemonBtn.dataset.action;
                if (action) {
                    await this.handleSystemStatusAction(action);
                }
            });
        }

        const budgetBtn = container.querySelector('[data-action="edit-budget"]') as HTMLButtonElement;
        if (budgetBtn) {
            budgetBtn.addEventListener('click', async (e) => {
                e.preventDefault();
                await this.handleSystemStatusAction('edit-budget');
            });
        }

        // Listen for confirmation reset events (triggered when 5s timeout expires)
        container.addEventListener('daemon-confirm-reset', async () => {
            await this.renderSystemStatus();
        });
    }

    private async handleSystemStatusAction(action: string): Promise<void> {
        const { handleSystemStatusAction } = await import('./pulse/system-status.ts');
        const executed = await handleSystemStatusAction(action);

        // If action wasn't executed (waiting for confirmation), re-render to show confirm state
        if (!executed) {
            await this.renderSystemStatus();
        }
    }

    /**
     * Update daemon status and re-render system status section if visible
     * Called by WebSocket handler when daemon_status messages arrive
     */
    public async updateDaemonStatus(data: DaemonStatusMessage): Promise<void> {
        currentDaemonStatus = data;

        if (this.isVisible) {
            await this.renderSystemStatus();
        }
    }

    /**
     * Clean up event subscriptions when panel is destroyed
     */
    protected onDestroy(): void {
        this.unsubscribers.forEach(unsub => unsub());
        this.unsubscribers = [];
    }
}

// Create global instance
// TODO(issue #16): Refactor global window pollution
// Replace with event delegation and custom events for cross-panel communication
const pulsePanel = new PulsePanel();
(window as any).pulsePanel = pulsePanel;

export function showPulsePanel(): void {
    pulsePanel.show();
}

export function togglePulsePanel(): void {
    pulsePanel.toggle();
}

/**
 * Update pulse panel with new daemon status
 * Called by WebSocket handler in websocket-handlers/daemon-status.ts
 */
export function updatePulsePanelDaemonStatus(data: DaemonStatusMessage): void {
    if (pulsePanelInstance) {
        pulsePanelInstance.updateDaemonStatus(data);
    }
}
