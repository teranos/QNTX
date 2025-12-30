/**
 * Job Detail Panel - Shows execution history for a scheduled job
 *
 * Displays:
 * - Job details (ATS code, interval, state)
 * - Execution history list (paginated)
 * - Individual execution details and logs
 */

import { debugLog } from '../debug.ts';
import type { ScheduledJob } from './types.ts';
import type { PulseExecution, JobStagesResponse, TaskLogsResponse, JobChildrenResponse } from './execution-types.ts';
import {
  listExecutions,
  getJobStages,
  getTaskLogsForJob,
  getJobChildren,
  formatDuration,
  formatRelativeTime,
  getStatusColorClass,
} from './execution-api.ts';
import { forceTriggerJob } from './api.ts';
import { showErrorDialog } from '../error-dialog.ts';

class JobDetailPanel {
  private panel: HTMLElement | null = null;
  private overlay: HTMLElement | null = null;
  private isVisible: boolean = false;
  private currentJob: ScheduledJob | null = null;
  private executions: PulseExecution[] = [];
  private currentPage: number = 0;
  private pageSize: number = 20;
  private totalExecutions: number = 0;
  private expandedExecutions: Set<string> = new Set();
  private executionStages: Map<string, JobStagesResponse> = new Map();
  private executionChildren: Map<string, JobChildrenResponse> = new Map();
  private expandedChildren: Set<string> = new Set();
  private childStages: Map<string, JobStagesResponse> = new Map();
  private expandedTasks: Set<string> = new Set();
  private taskLogs: Map<string, TaskLogsResponse> = new Map();
  private loadingTasks: Set<string> = new Set(); // Track tasks currently being loaded

  constructor() {
    this.initialize();
  }

  private initialize(): void {
    // Create overlay
    this.overlay = document.createElement('div');
    this.overlay.className = 'panel-overlay job-detail-overlay';
    this.overlay.addEventListener('click', () => this.hide());
    document.body.appendChild(this.overlay);

    // Create panel
    this.panel = document.createElement('div');
    this.panel.className = 'panel-slide-left job-detail-panel';
    document.body.appendChild(this.panel);

    // Close on Escape key
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape' && this.isVisible) {
        this.hide();
      }
    });
  }

  public async show(job: ScheduledJob): Promise<void> {
    if (!this.panel || !this.overlay) return;

    this.currentJob = job;
    this.currentPage = 0;

    debugLog('[Job Detail] Showing panel for job:', job.id);

    // Show panel and overlay
    this.panel.classList.add('visible');
    this.overlay.classList.add('visible');
    this.isVisible = true;

    // Load execution history
    await this.loadExecutions();
  }

  public hide(): void {
    if (!this.panel || !this.overlay) return;

    this.panel.classList.remove('visible');
    this.overlay.classList.remove('visible');
    this.isVisible = false;
    this.currentJob = null;
  }

  private async loadExecutions(): Promise<void> {
    if (!this.currentJob) return;

    try {
      const response = await listExecutions(this.currentJob.id, {
        limit: this.pageSize,
        offset: this.currentPage * this.pageSize,
      });

      this.executions = response.executions;
      this.totalExecutions = response.total;

      this.render();
    } catch (error) {
      console.error('[Job Detail] Failed to load executions:', error);
      this.renderError('Failed to load execution history');
    }
  }

  private render(): void {
    if (!this.panel || !this.currentJob) return;

    const hasMore = (this.currentPage + 1) * this.pageSize < this.totalExecutions;
    const hasPrev = this.currentPage > 0;

    const html = `
      <div class="job-detail-header">
        <button class="job-detail-back" onclick="window.jobDetailPanel.hide()">← Back</button>
        <h3>Job History</h3>
        <button class="panel-close" onclick="window.jobDetailPanel.hide()">✕</button>
      </div>

      <div class="job-detail-content">
        <!-- Job Info -->
        <div class="panel-card job-info-card">
          <div class="job-info-row">
            <span class="job-info-label">ATS Code:</span>
            <code class="job-info-value">${this.escapeHtml(this.currentJob.ats_code)}</code>
          </div>
          <div class="job-info-row">
            <span class="job-info-label">Interval:</span>
            <span class="job-info-value">${this.formatInterval(this.currentJob.interval_seconds)}</span>
          </div>
          <div class="job-info-row">
            <span class="job-info-label">State:</span>
            <span class="job-info-value job-state-${this.escapeHtml(this.currentJob.state)}">${this.escapeHtml(this.currentJob.state)}</span>
          </div>
          <div class="job-info-actions">
            <button class="force-trigger-btn" onclick="window.jobDetailPanel.handleForceTrigger()">
              Force Trigger
            </button>
          </div>
        </div>

        <!-- Execution List -->
        <div class="execution-history-section">
          <div class="section-header">
            <h4>Execution History</h4>
            <span class="execution-count">${this.totalExecutions} total</span>
          </div>

          ${this.executions.length === 0 ? this.renderEmpty() : this.renderExecutionList()}
        </div>

        <!-- Pagination -->
        ${this.totalExecutions > this.pageSize ? this.renderPagination(hasPrev, hasMore) : ''}
      </div>
    `;

    this.panel.innerHTML = html;

    // Attach event listeners to view job buttons
    this.attachEventListeners();
  }

  private attachEventListeners(): void {
    if (!this.panel) return;

    // Execution card expand/collapse
    const headers = this.panel.querySelectorAll('.execution-header[data-action="toggle-expand"]');
    headers.forEach(header => {
      header.addEventListener('click', async (e) => {
        e.stopPropagation();
        const card = (e.currentTarget as HTMLElement).closest('.execution-card') as HTMLElement;
        const executionId = card?.dataset.executionId;
        if (executionId) {
          await this.toggleExecutionExpand(executionId);
        }
      });
    });
  }

  private async toggleExecutionExpand(executionId: string): Promise<void> {
    const isExpanded = this.expandedExecutions.has(executionId);

    if (isExpanded) {
      // Collapse
      this.expandedExecutions.delete(executionId);
      this.render();
    } else {
      // Expand and fetch stages if not already loaded
      this.expandedExecutions.add(executionId);
      this.render();

      // Find the execution and get async_job_id
      const execution = this.executions.find(e => e.id === executionId);
      if (execution?.async_job_id && !this.executionStages.has(executionId)) {
        await this.loadExecutionStages(executionId, execution.async_job_id);
      }
    }
  }

  private async loadExecutionStages(executionId: string, jobId: string): Promise<void> {
    debugLog('[Job Detail] Loading stages for execution:', executionId, 'job:', jobId);

    try {
      const stages = await getJobStages(jobId);
      this.executionStages.set(executionId, stages);

      // If no stages, try loading children (job might be an orchestrator)
      if (stages.stages.length === 0) {
        debugLog('[Job Detail] No stages found, loading children for job:', jobId);
        const children = await getJobChildren(jobId);
        this.executionChildren.set(executionId, children);
      }

      // Re-render to show loaded stages or children
      this.render();
    } catch (error) {
      console.error('[Job Detail] Failed to load stages:', error);
      // Store empty stages response on error
      this.executionStages.set(executionId, {
        job_id: jobId,
        stages: []
      });
      this.render();
    }
  }

  private async toggleTaskExpand(jobId: string, taskId: string): Promise<void> {
    const taskKey = `${jobId}:${taskId}`;
    const isExpanded = this.expandedTasks.has(taskKey);

    if (isExpanded) {
      this.expandedTasks.delete(taskKey);
      this.render();
    } else {
      this.expandedTasks.add(taskKey);
      this.render();

      if (!this.taskLogs.has(taskKey)) {
        await this.loadTaskLogs(jobId, taskId);
      }
    }
  }

  private async loadTaskLogs(jobId: string, taskId: string): Promise<void> {
    const taskKey = `${jobId}:${taskId}`;
    debugLog('[Job Detail] Loading logs for task:', {jobId, taskId});

    try {
      const logs = await getTaskLogsForJob(jobId, taskId);
      this.taskLogs.set(taskKey, logs);
      this.render();
    } catch (error) {
      console.error('[Job Detail] Failed to load task logs:', error);
      this.taskLogs.set(taskKey, {
        task_id: taskId,
        logs: []
      });
      this.render();
    }
  }

  private renderExecutionList(): string {
    return `
      <div class="execution-list">
        ${this.executions.map(exec => this.renderExecutionCard(exec)).join('')}
      </div>
    `;
  }

  private renderExecutionCard(exec: PulseExecution): string {
    const statusClass = getStatusColorClass(exec.status);
    const duration = exec.duration_ms ? formatDuration(exec.duration_ms) : '—';
    const timeAgo = formatRelativeTime(exec.started_at);
    const isExpanded = this.expandedExecutions.has(exec.id);
    const stages = this.executionStages.get(exec.id);
    const children = this.executionChildren.get(exec.id);

    return `
      <div class="panel-card execution-card ${statusClass} ${isExpanded ? 'expanded' : ''}" data-execution-id="${exec.id}">
        <div class="execution-header" data-action="toggle-expand">
          <span class="execution-expand-icon">${isExpanded ? '▼' : '▶'}</span>
          <span class="panel-badge execution-status execution-status-${this.escapeHtml(exec.status)}">${this.escapeHtml(exec.status)}</span>
          <span class="execution-time">${timeAgo}</span>
          <span class="execution-duration">${duration}</span>
        </div>

        ${exec.result_summary ? `
          <div class="execution-summary">
            ${this.escapeHtml(exec.result_summary)}
          </div>
        ` : ''}

        ${exec.error_message ? `
          <div class="execution-error">
            Error: ${this.escapeHtml(exec.error_message)}
          </div>
        ` : ''}

        ${exec.async_job_id ? `
          <div class="execution-meta">
            <span class="execution-meta-label">Async Job:</span>
            <code class="execution-job-id">${this.escapeHtml(exec.async_job_id.substring(0, 12))}...</code>
          </div>
        ` : ''}

        ${isExpanded ? `
          <div class="execution-stages-container">
            ${stages && children ?
              // Show children if stages are empty
              (stages.stages.length === 0 && children.children.length > 0 ?
                this.renderChildren(children) :
                this.renderStages(stages)
              ) :
              `<div class="execution-logs-loading">Loading...</div>`
            }
          </div>
        ` : ''}
      </div>
    `;
  }

  private renderStages(stagesResponse: JobStagesResponse, jobId?: string): string {
    if (stagesResponse.stages.length === 0) {
      return `<div class="execution-no-logs">No logs available for this execution</div>`;
    }

    const contextJobId = jobId || stagesResponse.job_id;

    return `
      <div class="execution-stages">
        ${stagesResponse.stages.map(stage => `
          <div class="execution-stage">
            <div class="stage-header">${this.escapeHtml(stage.stage)}</div>
            <div class="stage-logs-direct">
              ${stage.tasks.map(task => this.renderTaskLogsDirectly(task, contextJobId)).join('')}
            </div>
          </div>
        `).join('')}
      </div>
    `;
  }

  private renderTaskLogsDirectly(task: { task_id: string; log_count: number }, jobId: string): string {
    const taskKey = `${jobId}:${task.task_id}`;
    const logs = this.taskLogs.get(taskKey);

    // Auto-load logs if not already loaded
    if (!logs && !this.loadingTasks.has(taskKey)) {
      this.loadingTasks.add(taskKey);
      this.loadTaskLogs(jobId, task.task_id);
      return `<div class="task-logs-loading">Loading logs...</div>`;
    }

    if (!logs) {
      return `<div class="task-logs-loading">Loading logs...</div>`;
    }

    return this.renderTaskLogs(logs);
  }

  private renderChildren(childrenResponse: JobChildrenResponse): string {
    if (childrenResponse.children.length === 0) {
      return `<div class="execution-no-logs">No child jobs found</div>`;
    }

    return `
      <div class="execution-children">
        <div class="children-header">Child Jobs (${childrenResponse.children.length})</div>
        ${childrenResponse.children.map(child => {
          const isExpanded = this.expandedChildren.has(child.id);
          const childStagesData = this.childStages.get(child.id);

          return `
            <div class="execution-child ${isExpanded ? 'expanded' : ''}" data-child-id="${child.id}">
              <div class="child-header" onclick="window.jobDetailPanel.handleChildClick('${this.escapeHtml(child.id)}')">
                <span class="child-expand-icon">${isExpanded ? '▼' : '▶'}</span>
                <span class="child-handler">${this.escapeHtml(child.handler_name)}</span>
                <span class="child-status child-status-${this.escapeHtml(child.status)}">${this.escapeHtml(child.status)}</span>
                <span class="child-progress">${Math.round(child.progress_pct)}%</span>
              </div>
              <div class="child-meta">
                <span class="child-id">${this.escapeHtml(child.id.substring(0, 16))}...</span>
                <span class="child-source">${this.escapeHtml(child.source.substring(0, 50))}...</span>
              </div>
              ${child.error ? `
                <div class="child-error">Error: ${this.escapeHtml(child.error)}</div>
              ` : ''}
              ${isExpanded ? `
                <div class="child-stages-container">
                  ${childStagesData ? this.renderStages(childStagesData, child.id) : `
                    <div class="execution-logs-loading">Loading child job stages...</div>
                  `}
                </div>
              ` : ''}
            </div>
          `;
        }).join('')}
      </div>
    `;
  }

  public async handleChildClick(childId: string): Promise<void> {
    const isExpanded = this.expandedChildren.has(childId);

    if (isExpanded) {
      this.expandedChildren.delete(childId);
      this.render();
    } else {
      this.expandedChildren.add(childId);
      this.render();

      if (!this.childStages.has(childId)) {
        await this.loadChildStages(childId);
      }
    }
  }

  private async loadChildStages(childId: string): Promise<void> {
    debugLog('[Job Detail] Loading stages for child job:', childId);

    try {
      const stages = await getJobStages(childId);
      this.childStages.set(childId, stages);
      this.render();
    } catch (error) {
      console.error('[Job Detail] Failed to load child stages:', error);
      this.childStages.set(childId, {
        job_id: childId,
        stages: []
      });
      this.render();
    }
  }

  private renderTaskLogs(logsResponse: TaskLogsResponse): string {
    if (logsResponse.logs.length === 0) {
      return `<div class="task-no-logs">No logs for this task</div>`;
    }

    return `
      <div class="task-logs">
        ${logsResponse.logs.map(log => {
          const formattedTime = this.formatLogTimestamp(log.timestamp);
          const levelBadge = this.getLevelBadge(log.level);

          return `
            <div class="task-log-entry task-log-${this.escapeHtml(log.level)}">
              <span class="task-log-timestamp">${formattedTime}</span>
              <span class="task-log-level ${levelBadge}">${this.escapeHtml(log.level.toUpperCase())}</span>
              <span class="task-log-message">${this.escapeHtml(log.message)}</span>
            </div>
          `;
        }).join('')}
      </div>
    `;
  }

  private formatLogTimestamp(timestamp: string): string {
    const date = new Date(timestamp);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffSecs = Math.floor(diffMs / 1000);
    const diffMins = Math.floor(diffSecs / 60);
    const diffHours = Math.floor(diffMins / 60);

    // If today, show just time
    if (diffHours < 24 && date.getDate() === now.getDate()) {
      return date.toLocaleTimeString('en-US', {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false
      });
    }

    // If recent, show relative time
    if (diffMins < 60) {
      return `${diffMins}m ago`;
    } else if (diffHours < 24) {
      return `${diffHours}h ago`;
    } else {
      // Otherwise show date + time
      return date.toLocaleString('en-US', {
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit',
        hour12: false
      });
    }
  }

  private getLevelBadge(level: string): string {
    switch (level.toLowerCase()) {
      case 'error':
        return 'level-error';
      case 'warn':
      case 'warning':
        return 'level-warn';
      case 'info':
        return 'level-info';
      case 'debug':
        return 'level-debug';
      default:
        return 'level-default';
    }
  }

  public async handleTaskClick(jobId: string, taskId: string): Promise<void> {
    await this.toggleTaskExpand(jobId, taskId);
  }

  private renderEmpty(): string {
    return `
      <div class="execution-empty">
        <p>No executions yet</p>
        <p class="execution-hint">This job hasn't run yet. Check back after the first execution.</p>
      </div>
    `;
  }

  private renderError(message: string): void {
    if (!this.panel) return;

    // Build error display using DOM API for security
    this.panel.innerHTML = '';

    const header = document.createElement('div');
    header.className = 'job-detail-header';

    const backBtn = document.createElement('button');
    backBtn.className = 'job-detail-back';
    backBtn.textContent = '← Back';
    backBtn.onclick = () => this.hide();

    const title = document.createElement('h3');
    title.textContent = 'Job History';

    const closeBtn = document.createElement('button');
    closeBtn.className = 'panel-close';
    closeBtn.textContent = '✕';
    closeBtn.onclick = () => this.hide();

    header.appendChild(backBtn);
    header.appendChild(title);
    header.appendChild(closeBtn);

    const content = document.createElement('div');
    content.className = 'job-detail-content';

    const errorDiv = document.createElement('div');
    errorDiv.className = 'job-detail-error';
    errorDiv.textContent = message;

    content.appendChild(errorDiv);

    this.panel.appendChild(header);
    this.panel.appendChild(content);
  }

  private renderPagination(hasPrev: boolean, hasMore: boolean): string {
    const start = this.currentPage * this.pageSize + 1;
    const end = Math.min((this.currentPage + 1) * this.pageSize, this.totalExecutions);

    return `
      <div class="execution-pagination">
        <button
          class="pagination-btn"
          ${!hasPrev ? 'disabled' : ''}
          onclick="window.jobDetailPanel.previousPage()">
          ← Previous
        </button>

        <span class="pagination-info">
          ${start}-${end} of ${this.totalExecutions}
        </span>

        <button
          class="pagination-btn"
          ${!hasMore ? 'disabled' : ''}
          onclick="window.jobDetailPanel.nextPage()">
          Next →
        </button>
      </div>
    `;
  }

  public async nextPage(): Promise<void> {
    this.currentPage++;
    await this.loadExecutions();
  }

  public async previousPage(): Promise<void> {
    if (this.currentPage > 0) {
      this.currentPage--;
      await this.loadExecutions();
    }
  }

  public async handleForceTrigger(): Promise<void> {
    if (!this.currentJob) return;

    try {
      debugLog('[Job Detail] Force triggering job:', this.currentJob.ats_code);

      // Call API to create one-time force trigger job
      await forceTriggerJob(this.currentJob.ats_code);

      // Reload executions to show the new forced execution
      await this.loadExecutions();

      debugLog('[Job Detail] Force trigger successful');
    } catch (error) {
      console.error('[Job Detail] Force trigger failed:', error);
      showErrorDialog(
        'Force trigger failed',
        error instanceof Error ? error.message : 'Unknown error'
      );
    }
  }

  private formatInterval(seconds: number): string {
    if (seconds < 60) return `${seconds}s`;
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes}m`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}h`;
    const days = Math.floor(hours / 24);
    return `${days}d`;
  }

  private escapeHtml(text: string): string {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
  }

  // ==========================================================================
  // Real-time update methods (called by WebSocket handlers)
  // ==========================================================================

  /**
   * Check if panel is currently showing a specific job
   */
  public isShowingJob(jobId: string): boolean {
    return this.isVisible && this.currentJob !== null && this.currentJob.id === jobId;
  }

  /**
   * Add a newly started execution to the list
   */
  public addStartedExecution(execution: Partial<PulseExecution>): void {
    if (!this.isShowingJob(execution.scheduled_job_id!)) return;

    // Add to start of executions array
    this.executions.unshift(execution as PulseExecution);
    this.totalExecutions++;

    // Re-render
    this.render();
  }

  /**
   * Update execution status (for failed/completed events)
   */
  public updateExecutionStatus(executionId: string, updates: Partial<PulseExecution>): void {
    if (!this.isVisible) return;

    const execution = this.executions.find(e => e.id === executionId);
    if (!execution) return;

    // Update execution fields
    Object.assign(execution, updates);

    // Re-render
    this.render();
  }

  /**
   * Append log chunk to execution (for log streaming)
   */
  public appendExecutionLog(executionId: string, logChunk: string): void {
    if (!this.isVisible) return;

    const execution = this.executions.find(e => e.id === executionId);
    if (!execution) return;

    // Append to existing logs or create new logs field
    if (execution.logs) {
      execution.logs += logChunk;
    } else {
      execution.logs = logChunk;
    }

    // Re-render if log viewer is showing this execution
    // (future enhancement - for now just update the data)
  }
}

// Create global instance
// TODO(issue #16): Refactor global window pollution
// Replace with event delegation and custom events for cross-panel communication
const jobDetailPanel = new JobDetailPanel();
(window as any).jobDetailPanel = jobDetailPanel;

export function showJobDetail(job: ScheduledJob): void {
  jobDetailPanel.show(job);
}
