/**
 * Hixtory Panel - IX operation history
 *
 * Displays async jobs when clicking ix (⨳) in the symbol palette:
 * - Shows job progress (15/20 operations completed)
 * - Displays cost tracking ($0.030 / $5.00 daily budget)
 * - Provides pause/resume controls for running jobs
 * - Keeps completed jobs visible for result exploration
 * - Click completed jobs to view results in graph
 * - Updates in real-time via WebSocket job_update messages
 *
 * Design based on docs/development/pulse-async-ix.md - Phase 3
 */

import { BasePanel } from './base-panel.ts';
import type { JobUpdateData, LLMStreamData } from '../types/websocket';
import type { Job as BackendJob } from '../../types/generated/typescript';
import { toast } from './toast';
import { IX } from '@generated/sym.js';

// Extended Job type with frontend-specific fields
interface Job extends BackendJob {
    cost_usd?: number;
    _graph_query?: string;
    type?: string;  // Legacy alias for handler_name
    metadata?: {
        total_operations?: number;
        completed_operations?: number;
        stage_message?: string;
        current_stage?: string;
        tasks?: Task[];
        command?: string;
        query?: string;
        [key: string]: any;
    };
}

interface Task {
    id: string;
    name?: string;
    status: 'pending' | 'queued' | 'running' | 'completed' | 'failed';
    created_at: number;
    result?: string;
    cost_actual?: number;
}

class JobListPanel extends BasePanel {
    private jobs: Map<string, Job> = new Map();

    constructor() {
        super({
            id: 'job-list-panel',
            classes: ['job-list-panel'],
            useOverlay: false,  // No overlay, uses click-outside
            closeOnEscape: true,
            insertAfter: '#symbolPalette'
        });
    }

    protected getTemplate(): string {
        return `
            <div class="job-list-header">
                <h3 class="job-list-title">${IX} Hixtory <span class="hixtory-count">(<span id="hixtory-count">0</span>)</span></h3>
                <button class="job-list-close" aria-label="Close">✕</button>
            </div>
            <div class="job-list-content" id="job-list-content">
                <div class="panel-empty job-list-empty">
                    <p>No IX operations yet</p>
                    <p class="job-list-hint">Run an IX command to start</p>
                </div>
            </div>
        `;
    }

    protected setupEventListeners(): void {
        // Note: Close button (.job-list-close) needs manual handling since it uses
        // a custom class. BasePanel only auto-handles .panel-close
        const closeBtn = this.$('.job-list-close');
        closeBtn?.addEventListener('click', () => this.hide());

        // New operation button
        const newBtn = this.$('#new-ix-operation');
        if (newBtn) {
            newBtn.addEventListener('click', () => {
                this.hide();
                const fileUploadInput = document.getElementById('file-upload');
                if (fileUploadInput) {
                    (fileUploadInput as HTMLInputElement).click();
                }
            });
        }
    }

    protected async onShow(): Promise<void> {
        // Fetch jobs from API on first show (if jobs map is empty)
        if (this.jobs.size === 0) {
            this.showLoading('Loading history...');
            await this.fetchJobs();
            this.hideLoading();
        }
        this.render();
    }

    /**
     * Fetch async jobs from /api/pulse/jobs API
     */
    private async fetchJobs(): Promise<void> {
        try {
            const response = await fetch('/api/pulse/jobs?limit=100');
            if (!response.ok) {
                console.error('Failed to fetch jobs:', response.statusText);
                return;
            }

            const data = await response.json();
            const jobs = data.jobs || [];

            jobs.forEach((job: Job) => {
                this.jobs.set(job.id, job);
            });

            console.log(`Loaded ${jobs.length} async jobs from API`);
        } catch (error) {
            console.error('Error fetching jobs:', error);
        }
    }

    /**
     * Handle job update from WebSocket
     */
    public handleJobUpdate(data: JobUpdateData): void {
        const job = data.job as Job;
        const previousJob = this.jobs.get(job.id);

        // Store graph_query from metadata if available
        if (data.metadata && data.metadata.graph_query) {
            job._graph_query = data.metadata.graph_query as string;
        }

        // Show toast notifications for important state changes
        this.notifyJobStateChange(previousJob, job);

        // Update jobs map
        this.jobs.set(job.id, job);

        // Re-render if panel is visible
        if (this.isVisible) {
            this.render();
        }
    }

    /**
     * Show toast notifications for important job state changes
     */
    private notifyJobStateChange(previous: Job | undefined, current: Job): void {
        const jobName = current.id.substring(0, 8);

        // Job just paused - show reason
        if (current.status === 'paused' && previous?.status !== 'paused') {
            const pulseState = (current as any).pulse_state;
            const reason = pulseState?.pause_reason || 'unknown';

            if (reason === 'rate_limited') {
                toast.warning(`Job ${jobName} paused: Rate limit reached`);
            } else if (reason === 'budget_exceeded') {
                toast.warning(`Job ${jobName} paused: Budget limit exceeded`);
            } else if (reason === 'user_requested') {
                toast.info(`Job ${jobName} paused by user`);
            }
        }

        // Job failed - show error
        if (current.status === 'failed' && previous?.status !== 'failed') {
            const errorMsg = current.error ? `: ${current.error.substring(0, 50)}` : '';
            toast.error(`Job ${jobName} failed${errorMsg}`);
        }

        // Job completed - show success (only for top-level jobs, not tasks)
        if (current.status === 'completed' && previous?.status !== 'completed') {
            if (!(current as any).parent_job_id) {
                toast.success(`Job ${jobName} completed`);
            }
        }
    }

    // Handle streaming LLM output - display live tokens
    public handleLLMStream(data: LLMStreamData): void {
        const { job_id, content, done, model, stage, error } = data;

        if (!job_id) return;

        // Find job element
        const jobElement = document.querySelector(`[data-job-id="${job_id}"]`);
        if (!jobElement) {
            console.warn('Job element not found for streaming:', job_id);
            return;
        }

        // Find or create stream output container
        let streamContainer = jobElement.querySelector('.llm-stream-output') as HTMLElement | null;
        if (!streamContainer) {
            streamContainer = document.createElement('div');
            streamContainer.className = 'llm-stream-output';
            streamContainer.dataset.jobId = job_id;

            // Insert after job progress
            const progressSection = jobElement.querySelector('.job-progress');
            if (progressSection && progressSection.parentElement) {
                progressSection.parentElement.appendChild(streamContainer);
            } else {
                jobElement.appendChild(streamContainer);
            }

            // Add model info header
            if (model) {
                const header = document.createElement('div');
                header.className = 'stream-header';
                header.textContent = `🤖 ${model} - ${stage || 'streaming'}`;
                streamContainer.appendChild(header);
            }

            // Add content container
            const contentDiv = document.createElement('div');
            contentDiv.className = 'stream-content';
            streamContainer.appendChild(contentDiv);
        }

        const contentDiv = streamContainer.querySelector('.stream-content');

        // Handle errors
        if (error) {
            streamContainer.classList.add('stream-error');
            const errorDiv = document.createElement('div');
            errorDiv.className = 'stream-error-message';
            errorDiv.textContent = `Error: ${error}`;
            if (contentDiv) contentDiv.appendChild(errorDiv);
            return;
        }

        // Append content tokens
        if (content && content.length > 0 && contentDiv) {
            const textNode = document.createTextNode(content);
            contentDiv.appendChild(textNode);
            contentDiv.scrollTop = contentDiv.scrollHeight;
        }

        // Handle completion
        if (done) {
            streamContainer.classList.add('stream-complete');
            streamContainer.classList.remove('stream-active');
        } else if (!streamContainer.classList.contains('stream-active')) {
            streamContainer.classList.add('stream-active');
        }
    }

    /**
     * Render hixtory - compact list of job executions
     */
    private render(): void {
        const content = this.$('#job-list-content');
        const countSpan = this.$('#hixtory-count');

        if (!content) return;

        if (this.jobs.size === 0) {
            content.innerHTML = '';
            content.appendChild(
                this.createEmptyState('No IX operations yet', 'Run an IX command to start')
            );
            // Add panel-specific class for styling
            content.firstElementChild?.classList.add('job-list-empty');

            if (countSpan) countSpan.textContent = '0';
            return;
        }

        // Get all jobs, sort by created_at descending (most recent first)
        const allJobs = Array.from(this.jobs.values())
            .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime());

        if (countSpan) {
            countSpan.textContent = allJobs.length.toString();
        }

        content.innerHTML = '';

        allJobs.forEach(job => {
            const item = this.renderHistoryItem(job);
            content.appendChild(item);
        });
    }

    /**
     * Render a single hixtory item (compact format)
     */
    private renderHistoryItem(job: Job): HTMLElement {
        const statusClass = this.getStatusClass(job.status);
        const timeAgo = this.formatRelativeTime(job.created_at);
        const command = this.getJobCommand(job);

        const item = document.createElement('div');
        item.className = 'hixtory-item';
        item.dataset.jobId = job.id;

        const commandDiv = document.createElement('div');
        commandDiv.className = 'hixtory-command';
        commandDiv.textContent = command;

        const metaDiv = document.createElement('div');
        metaDiv.className = 'hixtory-meta';

        const statusSpan = document.createElement('span');
        statusSpan.className = `hixtory-status ${statusClass}`;
        statusSpan.textContent = job.status;

        const timeSpan = document.createElement('span');
        timeSpan.className = 'hixtory-time';
        timeSpan.textContent = timeAgo;

        metaDiv.appendChild(statusSpan);
        metaDiv.appendChild(timeSpan);

        item.appendChild(commandDiv);
        item.appendChild(metaDiv);

        return item;
    }

    /**
     * Get display command for a job
     */
    private getJobCommand(job: Job): string {
        if (job.metadata?.command) return job.metadata.command;
        if (job.metadata?.query) return job.metadata.query;
        if (job.source) return job.source;

        const handlerName = job.handler_name || job.type || 'Unknown';
        return `${handlerName} (${job.id.substring(0, 8)})`;
    }

    /**
     * Get CSS class for job status
     */
    private getStatusClass(status: string): string {
        if (status === 'completed') return 'success';
        if (status === 'failed') return 'error';
        if (status === 'running' || status === 'queued') return 'running';
        return '';
    }

    /**
     * Format ISO 8601 timestamp as relative time (e.g., "5m ago")
     */
    private formatRelativeTime(timestamp: string): string {
        const date = new Date(timestamp);
        const now = Date.now();
        const diff = now - date.getTime();
        const seconds = Math.floor(diff / 1000);
        const minutes = Math.floor(seconds / 60);
        const hours = Math.floor(minutes / 60);
        const days = Math.floor(hours / 24);

        if (days > 0) return `${days}d ago`;
        if (hours > 0) return `${hours}h ago`;
        if (minutes > 0) return `${minutes}m ago`;
        return 'just now';
    }
}

// Initialize and export
const jobListPanel = new JobListPanel();

export function showJobList(): void {
    jobListPanel.show();
}

export function hideJobList(): void {
    jobListPanel.hide();
}

export function toggleJobList(): void {
    jobListPanel.toggle();
}

export function handleJobUpdate(data: JobUpdateData): void {
    jobListPanel.handleJobUpdate(data);
}

// Handle streaming LLM output
export function handleLLMStream(data: LLMStreamData): void {
    if (jobListPanel) {
        jobListPanel.handleLLMStream(data);
    }
}

export {};
