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

import type { JobUpdateData, JobDetailsData, LLMStreamData } from '../types/websocket';
import type { Job as CoreJob } from '../types/core';

interface Job extends CoreJob {
    cost_usd?: number;
    source?: string;
    _graph_query?: string;
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


class JobListPanel {
    private panel: HTMLElement | null = null;
    private isVisible: boolean = false;
    private jobs: Map<string, Job> = new Map();

    constructor() {
        this.initialize();
    }

    private initialize(): void {
        // Create panel element
        this.panel = document.createElement('div');
        this.panel.id = 'job-list-panel';
        this.panel.className = 'job-list-panel hidden';
        this.panel.innerHTML = this.getTemplate();

        // Insert after symbol palette
        const symbolPalette = document.getElementById('symbolPalette');
        if (symbolPalette && symbolPalette.parentNode) {
            symbolPalette.parentNode.insertBefore(this.panel, symbolPalette.nextSibling);
        }

        // Click outside to close
        document.addEventListener('click', (e: MouseEvent) => {
            const target = e.target as HTMLElement;
            if (this.isVisible && this.panel && !this.panel.contains(target) && !target.closest('.palette-cell[data-cmd="ix"]')) {
                this.hide();
            }
        });

        // Setup event listeners
        this.setupEventListeners();
    }

    private getTemplate(): string {
        return `
            <div class="job-list-header">
                <h3 class="job-list-title">⨳ Hixtory <span class="hixtory-count">(<span id="hixtory-count">0</span>)</span></h3>
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

    private setupEventListeners(): void {
        if (!this.panel) return;

        // Close button
        const closeBtn = this.panel.querySelector('.job-list-close');
        if (closeBtn) {
            closeBtn.addEventListener('click', () => this.hide());
        }

        // New operation button
        const newBtn = this.panel.querySelector('#new-ix-operation');
        if (newBtn) {
            newBtn.addEventListener('click', () => {
                this.hide();
                // Trigger file upload
                const fileUploadInput = document.getElementById('file-upload');
                if (fileUploadInput) {
                    (fileUploadInput as HTMLInputElement).click();
                }
            });
        }
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

            // Populate jobs map
            jobs.forEach((job: Job) => {
                this.jobs.set(job.id, job);
            });

            console.log(`Loaded ${jobs.length} async jobs from API`);
        } catch (error) {
            console.error('Error fetching jobs:', error);
        }
    }

    public async show(): Promise<void> {
        if (!this.panel) return;
        this.isVisible = true;
        this.panel.classList.remove('hidden');
        this.panel.classList.add('visible');

        // Fetch jobs from API on first show (if jobs map is empty)
        if (this.jobs.size === 0) {
            await this.fetchJobs();
        }

        this.render();
    }

    public hide(): void {
        if (!this.panel) return;
        this.isVisible = false;
        this.panel.classList.remove('visible');
        this.panel.classList.add('hidden');
    }

    public toggle(): void {
        if (this.isVisible) {
            this.hide();
        } else {
            this.show();
        }
    }

    /**
     * Handle job update from WebSocket
     */
    public handleJobUpdate(data: JobUpdateData): void {
        const job = data.job as Job;

        // Store graph_query from metadata if available
        if (data.metadata && data.metadata.graph_query) {
            job._graph_query = data.metadata.graph_query;
        }

        // Update jobs map
        this.jobs.set(job.id, job);

        // Keep completed/failed jobs visible for exploration
        // (removed auto-delete timer - users can manually dismiss jobs)

        // Re-render if panel is visible
        if (this.isVisible) {
            this.render();
        }
    }

    // Handle streaming LLM output - display live tokens
    public handleLLMStream(data: LLMStreamData): void {
        const { job_id, task_id, content, done, model, stage, error } = data;

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
            // Create text node for efficient DOM updates
            const textNode = document.createTextNode(content);
            contentDiv.appendChild(textNode);

            // Auto-scroll to bottom
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
        if (!this.panel) return;

        const content = this.panel.querySelector('#job-list-content');
        const countSpan = this.panel.querySelector('#hixtory-count');

        if (!content) return;

        if (this.jobs.size === 0) {
            // Build empty state using DOM API
            const emptyDiv = document.createElement('div');
            emptyDiv.className = 'panel-empty job-list-empty';

            const p1 = document.createElement('p');
            p1.textContent = 'No IX operations yet';

            const p2 = document.createElement('p');
            p2.className = 'job-list-hint';
            p2.textContent = 'Run an IX command to start';

            emptyDiv.appendChild(p1);
            emptyDiv.appendChild(p2);

            content.innerHTML = '';
            content.appendChild(emptyDiv);

            if (countSpan) countSpan.textContent = '0';
            return;
        }

        // Get all jobs, sort by created_at descending (most recent first)
        const allJobs = Array.from(this.jobs.values())
            .sort((a, b) => b.created_at - a.created_at);

        // Update count
        if (countSpan) {
            countSpan.textContent = allJobs.length.toString();
        }

        // Build hixtory items using DOM API for security
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
        // Try to get command from metadata
        if (job.metadata?.command) return job.metadata.command;
        if (job.metadata?.query) return job.metadata.query;

        // Fallback: construct from source and type
        if (job.source) return job.source;

        // Last resort: use job type
        return `${job.type} (${job.id.substring(0, 8)})`;
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
     * Format timestamp as relative time (e.g., "5m ago")
     */
    private formatRelativeTime(timestamp: number): string {
        const now = Date.now();
        const diff = now - timestamp;
        const seconds = Math.floor(diff / 1000);
        const minutes = Math.floor(seconds / 60);
        const hours = Math.floor(minutes / 60);
        const days = Math.floor(hours / 24);

        if (days > 0) return `${days}d ago`;
        if (hours > 0) return `${hours}h ago`;
        if (minutes > 0) return `${minutes}m ago`;
        return 'just now';
    }

    /**
     * Escape HTML to prevent XSS
     */
    private escapeHtml(text: string): string {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
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
