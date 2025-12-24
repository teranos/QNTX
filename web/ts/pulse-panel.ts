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

import { debugLog } from './debug';
import type { ScheduledJob } from './pulse/types';
import { listScheduledJobs, pauseScheduledJob, resumeScheduledJob, deleteScheduledJob, forceTriggerJob } from './pulse/api';
import { formatInterval } from './pulse/types';
import { toast } from './toast';
import { showErrorDialog } from './error-dialog';
import type { PulseExecution } from './pulse/execution-types';
import { listExecutions } from './pulse/execution-api';
import { PulsePanelState } from './pulse/panel-state';
import * as PanelRenderer from './pulse/panel';
import type { PanelEventHandlers } from './pulse/panel-events';
import { attachPanelEventListeners } from './pulse/panel-events';
import * as RealtimeHandlers from './pulse/realtime-handlers';
import type { DaemonStatusMessage } from '../types/websocket';

// Global daemon status (updated via WebSocket)
let currentDaemonStatus: DaemonStatusMessage | null = null;

// Global pulse panel instance
let pulsePanelInstance: PulsePanel | null = null;

class PulsePanel {
    private panel: HTMLElement | null = null;
    private overlay: HTMLElement;
    private isVisible: boolean = false;
    private jobs: Map<string, ScheduledJob> = new Map();
    // Inline execution view state
    private state: PulsePanelState;

    constructor() {
        // Create overlay element
        this.overlay = document.createElement('div');
        this.overlay.className = 'panel-overlay pulse-panel-overlay';
        this.overlay.addEventListener('click', () => this.hide());
        document.body.appendChild(this.overlay);

        // Initialize state manager
        this.state = new PulsePanelState();

        // Store global reference
        pulsePanelInstance = this;

        this.initialize();
    }

    private initialize(): void {
        // Create panel element
        this.panel = document.createElement('div');
        this.panel.id = 'pulse-panel';
        this.panel.className = 'panel-slide-left pulse-panel';
        this.panel.innerHTML = PanelRenderer.renderPanelTemplate();
        document.body.appendChild(this.panel);

        // Click outside to close (handled by overlay)
        // Kept for palette cell clicks
        document.addEventListener('click', (e: MouseEvent) => {
            const target = e.target as HTMLElement;
            if (this.panel && this.isVisible && !this.panel.contains(target) && !target.closest('.palette-cell[data-cmd="pulse"]')) {
                this.hide();
            }
        });
    }

    public async toggle(): Promise<void> {
        if (this.isVisible) {
            this.hide();
        } else {
            await this.show();
        }
    }

    public async show(): Promise<void> {
        if (!this.panel) return;

        this.panel.classList.add('visible');
        this.overlay.classList.add('visible');
        this.isVisible = true;

        // Load jobs
        await this.loadJobs();
    }

    public hide(): void {
        if (!this.panel) return;

        this.panel.classList.remove('visible');
        this.overlay.classList.remove('visible');
        this.isVisible = false;
    }

    private async loadJobs(): Promise<void> {
        const content = this.panel?.querySelector('.pulse-panel-content');
        if (!content) return;

        try {
            const jobs = await listScheduledJobs();

            this.jobs.clear();
            jobs.forEach(job => this.jobs.set(job.id, job));

            // Clean up orphaned job IDs from expandedJobs (memory leak prevention)
            this.state.cleanupOrphanedJobs(new Set(this.jobs.keys()));

            this.render();
        } catch (error) {
            console.error('[Pulse Panel] Failed to load jobs:', error);

            // Build error display using DOM API for security
            const errorDiv = document.createElement('div');
            errorDiv.className = 'panel-error pulse-error';
            errorDiv.textContent = `Failed to load scheduled jobs: ${(error as Error).message}`;

            content.innerHTML = '';
            content.appendChild(errorDiv);
        }
    }

    private async render(): Promise<void> {
        if (!this.panel) return;

        // Render System Status section
        await this.renderSystemStatus();

        // Render Active Queue section
        await this.renderActiveQueue();

        // Render Schedules section
        this.renderSchedules();
    }

    private async renderSystemStatus(): Promise<void> {
        const container = this.panel?.querySelector('#pulse-system-status-content');
        if (!container) return;

        // Import and use system-status renderer
        const { renderSystemStatus } = await import('./pulse/system-status.ts');

        // Use current daemon status or defaults
        const daemonStatus = {
            running: currentDaemonStatus?.running ?? false,
            budget_daily: currentDaemonStatus?.budget_daily ?? 0,
            budget_daily_limit: currentDaemonStatus?.budget_daily_limit ?? 5.0,
            budget_monthly: currentDaemonStatus?.budget_monthly ?? 0,
            budget_monthly_limit: currentDaemonStatus?.budget_monthly_limit ?? 50.0
        };

        container.innerHTML = renderSystemStatus(daemonStatus);

        // Attach event handlers for system status actions
        this.attachSystemStatusHandlers();
    }

    private async renderActiveQueue(): Promise<void> {
        const container = this.panel?.querySelector('#pulse-active-queue-content');
        if (!container) return;

        // Import and fetch active jobs
        const { renderActiveQueue, fetchActiveJobs } = await import('./pulse/active-queue.ts');
        const activeJobs = await fetchActiveJobs();

        container.innerHTML = renderActiveQueue(activeJobs);
    }

    private renderSchedules(): void {
        const container = this.panel?.querySelector('#pulse-schedules-content');
        if (!container) return;

        // Import schedules renderer
        import('./pulse/schedules.ts').then(({ renderEmptyState, renderJobCard }) => {
            if (this.jobs.size === 0) {
                container.innerHTML = renderEmptyState();
                return;
            }

            const jobsHtml = Array.from(this.jobs.values())
                .sort((a, b) => new Date(a.created_at).getTime() - new Date(b.created_at).getTime())
                .map(job => renderJobCard(job, this.state))
                .join('');

            container.innerHTML = `<div class="pulse-jobs-list">${jobsHtml}</div>`;

            // Attach event listeners
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

    // ========================================================================
    // System Status Event Handlers
    // ========================================================================

    private attachSystemStatusHandlers(): void {
        if (!this.panel) return;

        const container = this.panel.querySelector('#pulse-system-status-content');
        if (!container) return;

        // Start/Stop daemon button
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

        // Edit budget button
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

    // ========================================================================
    // Event handler methods (called by panel-events.ts)
    // ========================================================================

    private async handleLoadMore(jobId: string): Promise<void> {
        // Increase limit by 10
        const currentLimit = this.state.executionLimits.get(jobId) || 5;
        this.state.executionLimits.set(jobId, currentLimit + 10);
        this.render();
    }

    private async handleRetryExecutions(jobId: string): Promise<void> {
        // Clear error and retry
        this.state.executionErrors.delete(jobId);
        await this.loadExecutionsForJob(jobId);
    }

    private async handleViewDetailed(jobId: string): Promise<void> {
        const job = this.jobs.get(jobId);
        if (!job) return;

        // Open the original job detail panel
        const { showJobDetail } = await import('./pulse/job-detail-panel.js');
        showJobDetail(job);
    }

    private async toggleJobExpansion(jobId: string): Promise<void> {
        if (this.state.expandedJobs.has(jobId)) {
            // Collapse
            this.state.expandedJobs.delete(jobId);
            this.state.saveToLocalStorage();
            this.render();
        } else {
            // Expand
            this.state.expandedJobs.add(jobId);
            this.state.saveToLocalStorage();
            this.render();

            // Load executions if not already loaded
            if (!this.state.jobExecutions.has(jobId) && !this.state.loadingExecutions.has(jobId)) {
                await this.loadExecutionsForJob(jobId);
            }
        }
    }

    private async loadExecutionsForJob(jobId: string): Promise<void> {
        this.state.loadingExecutions.add(jobId);
        this.state.executionErrors.delete(jobId); // Clear previous error
        this.render();

        try {
            const response = await listExecutions(jobId, { limit: 20, offset: 0 });
            this.state.jobExecutions.set(jobId, response.executions);
            this.state.executionErrors.delete(jobId); // Clear error on success
        } catch (error) {
            console.error('[Pulse Panel] Failed to load executions:', error);
            const errorMessage = (error as Error).message || 'Unknown error';
            this.state.executionErrors.set(jobId, errorMessage); // Store error for display
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

            // Call force trigger API
            await forceTriggerJob(job.ats_code);

            // Expand the job if collapsed
            if (!this.state.expandedJobs.has(jobId)) {
                this.state.expandedJobs.add(jobId);
                this.state.saveToLocalStorage();
            }

            // Reload executions to show the new one
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

            // Reload jobs
            await this.loadJobs();
        } catch (error) {
            console.error(`[Pulse Panel] Failed to ${action} job:`, error);

            // Build detailed error message with context
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

        // Dynamically import prose panel to avoid circular dependencies
        const { showProseDocument } = await import('./prose/panel.js');

        // Open prose panel and navigate to document
        await showProseDocument(docId);

        // Keep pulse panel open so user can see both
        // User can manually close pulse panel if desired
    }

    /**
     * Update daemon status and re-render system status section if visible
     * Called by WebSocket handler when daemon_status messages arrive
     */
    public async updateDaemonStatus(data: DaemonStatusMessage): Promise<void> {
        // Store current daemon status
        currentDaemonStatus = data;

        // Update System Status section if panel is visible
        if (this.isVisible) {
            await this.renderSystemStatus();
        }
    }
}

// Create global instance
const pulsePanel = new PulsePanel();
(window as any).pulsePanel = pulsePanel;

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
