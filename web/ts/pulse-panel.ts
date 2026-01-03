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
import { debugLog } from './debug';
import type { ScheduledJobResponse } from './pulse/types';
import { listScheduledJobs, pauseScheduledJob, resumeScheduledJob, deleteScheduledJob, forceTriggerJob } from './pulse/api';
import { formatInterval } from './pulse/types';
import { toast } from './toast';
import { showErrorDialog } from './error-dialog';
import { listExecutions } from './pulse/execution-api';
import { PulsePanelState } from './pulse/panel-state';
import * as PanelRenderer from './pulse/panel';
import { attachPanelEventListeners } from './pulse/panel-events';
import type { DaemonStatusMessage } from '../types/websocket';
import {
    onExecutionStarted,
    onExecutionCompleted,
    onExecutionFailed,
    unixToISO,
} from './pulse/events';

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
     * Subscribe to Pulse execution events for real-time updates
     */
    private subscribeToEvents(): void {
        // Execution started - update last run time and add to inline list
        this.unsubscribers.push(
            onExecutionStarted((detail) => {
                if (!this.isVisible) return;

                const job = this.jobs.get(detail.scheduledJobId);
                const timestamp = unixToISO(detail.timestamp);
                if (job) {
                    job.last_run_at = timestamp;
                }

                // Add to inline execution list if job is expanded
                if (this.state.isExpanded(detail.scheduledJobId)) {
                    const executions = this.state.getExecutions(detail.scheduledJobId) || [];
                    executions.unshift({
                        id: detail.executionId,
                        scheduled_job_id: detail.scheduledJobId,
                        status: 'running',
                        started_at: timestamp,
                        created_at: timestamp,
                        updated_at: timestamp,
                    } as any);
                    this.state.setExecutions(detail.scheduledJobId, executions);
                }

                this.render();
            })
        );

        // Execution completed - update execution status
        this.unsubscribers.push(
            onExecutionCompleted((detail) => {
                if (!this.isVisible) return;

                const job = this.jobs.get(detail.scheduledJobId);
                const timestamp = unixToISO(detail.timestamp);
                if (job) {
                    job.last_run_at = timestamp;
                }

                // Update inline execution if expanded
                for (const [_jobId, executions] of this.state.jobExecutions.entries()) {
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

                this.render();
            })
        );

        // Execution failed - update execution status
        this.unsubscribers.push(
            onExecutionFailed((detail) => {
                if (!this.isVisible) return;

                const job = this.jobs.get(detail.scheduledJobId);
                const timestamp = unixToISO(detail.timestamp);
                if (job) {
                    job.last_run_at = timestamp;
                }

                // Update inline execution if expanded
                for (const [_jobId, executions] of this.state.jobExecutions.entries()) {
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

                this.render();
            })
        );
    }

    protected getTemplate(): string {
        return PanelRenderer.renderPanelTemplate();
    }

    protected setupEventListeners(): void {
        // Event listeners are attached dynamically in renderSchedules()
    }

    protected async onShow(): Promise<void> {
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

            this.render();
        } catch (error) {
            console.error('[Pulse Panel] Failed to load jobs:', error);

            const errorDiv = document.createElement('div');
            errorDiv.className = 'panel-error pulse-error';
            errorDiv.textContent = `Failed to load scheduled jobs: ${(error as Error).message}`;

            content.innerHTML = '';
            content.appendChild(errorDiv);
        }
    }

    private async render(): Promise<void> {
        await this.renderSystemStatus();
        await this.renderActiveQueue();
        this.renderSchedules();
    }

    private async renderSystemStatus(): Promise<void> {
        const container = this.$('#pulse-system-status-content');
        if (!container) return;

        const { renderSystemStatus } = await import('./pulse/system-status.ts');

        const daemonStatus = {
            running: currentDaemonStatus?.running ?? false,
            budget_daily: currentDaemonStatus?.budget_daily ?? 0,
            budget_daily_limit: currentDaemonStatus?.budget_daily_limit ?? 1.0,
            budget_weekly: currentDaemonStatus?.budget_weekly ?? 0,
            budget_weekly_limit: currentDaemonStatus?.budget_weekly_limit ?? 7.0,
            budget_monthly: currentDaemonStatus?.budget_monthly ?? 0,
            budget_monthly_limit: currentDaemonStatus?.budget_monthly_limit ?? 30.0
        };

        container.innerHTML = renderSystemStatus(daemonStatus);
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

            attachPanelEventListeners(this.panel!, {
                onToggleExpansion: (jobId) => this.toggleJobExpansion(jobId),
                onForceTrigger: (jobId) => this.handleForceTrigger(jobId),
                onJobAction: (jobId, action) => this.handleJobAction(jobId, action),
                onLoadMore: (jobId) => this.handleLoadMore(jobId),
                onRetryExecutions: (jobId) => this.handleRetryExecutions(jobId),
                onViewDetailed: (jobId) => this.handleViewDetailed(jobId),
                onProseLocation: (docId) => this.handleProseLocationClick(docId)
            });
        });
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
    }

    private async handleSystemStatusAction(action: string): Promise<void> {
        const { handleSystemStatusAction } = await import('./pulse/system-status.ts');
        await handleSystemStatusAction(action);
    }

    private async handleLoadMore(jobId: string): Promise<void> {
        const currentLimit = this.state.executionLimits.get(jobId) || 5;
        this.state.executionLimits.set(jobId, currentLimit + 10);
        this.render();
    }

    private async handleRetryExecutions(jobId: string): Promise<void> {
        this.state.executionErrors.delete(jobId);
        await this.loadExecutionsForJob(jobId);
    }

    private async handleViewDetailed(jobId: string): Promise<void> {
        const job = this.jobs.get(jobId);
        if (!job) return;

        const { showJobDetail } = await import('./pulse/job-detail-panel.js');
        showJobDetail(job);
    }

    private async toggleJobExpansion(jobId: string): Promise<void> {
        if (this.state.expandedJobs.has(jobId)) {
            this.state.expandedJobs.delete(jobId);
            this.state.saveToLocalStorage();
            this.render();
        } else {
            this.state.expandedJobs.add(jobId);
            this.state.saveToLocalStorage();
            this.render();

            if (!this.state.jobExecutions.has(jobId) && !this.state.loadingExecutions.has(jobId)) {
                await this.loadExecutionsForJob(jobId);
            }
        }
    }

    private async loadExecutionsForJob(jobId: string): Promise<void> {
        this.state.loadingExecutions.add(jobId);
        this.state.executionErrors.delete(jobId);
        this.render();

        try {
            const response = await listExecutions(jobId, { limit: 20, offset: 0 });
            this.state.jobExecutions.set(jobId, response.executions);
            this.state.executionErrors.delete(jobId);
        } catch (error) {
            console.error('[Pulse Panel] Failed to load executions:', error);
            const errorMessage = (error as Error).message || 'Unknown error';
            this.state.executionErrors.set(jobId, errorMessage);
            toast.error(`Failed to load execution history: ${errorMessage}`);
        } finally {
            this.state.loadingExecutions.delete(jobId);
            this.render();
        }
    }

    private async handleForceTrigger(jobId: string): Promise<void> {
        const job = this.jobs.get(jobId);
        if (!job) return;

        try {
            debugLog('[Pulse Panel] Force triggering job:', job.ats_code);

            await forceTriggerJob(job.ats_code);

            if (!this.state.expandedJobs.has(jobId)) {
                this.state.expandedJobs.add(jobId);
                this.state.saveToLocalStorage();
            }

            await this.loadExecutionsForJob(jobId);

            toast.success('Force trigger started - check execution history below');
        } catch (error) {
            console.error('[Pulse Panel] Force trigger failed:', error);
            showErrorDialog(
                'Force trigger failed',
                (error as Error).message
            );
        }
    }

    private async handleJobAction(jobId: string, action: string): Promise<void> {
        const job = this.jobs.get(jobId);

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

            await this.loadJobs();
        } catch (error) {
            console.error(`[Pulse Panel] Failed to ${action} job:`, error);

            let errorMsg = `Failed to ${action} job: ${(error as Error).message}`;

            if (job) {
                errorMsg += `\n\nATS Code:\n${job.ats_code}`;
                errorMsg += `\nInterval: ${formatInterval(job.interval_seconds)}`;
                if (job.created_from_doc) {
                    errorMsg += `\nDocument: ${job.created_from_doc}`;
                }
            }

            toast.error(errorMsg);
        }
    }

    private async handleProseLocationClick(docId: string): Promise<void> {
        debugLog('[Pulse Panel] Opening prose document:', docId);

        const { showProseDocument } = await import('./prose/panel.js');
        await showProseDocument(docId);
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
