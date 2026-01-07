/**
 * Schedules Section - Recurring timer jobs
 */

import type { ScheduledJobResponse } from './types';
import type { Execution } from './execution-types';
import type { PulsePanelState } from './panel-state';
import { formatInterval } from './types';
import { formatRelativeTime, escapeHtml, formatDuration } from './panel.ts';
import { Pulse } from '@generated/sym.js';

/**
 * Render empty state when no scheduled jobs exist
 */
export function renderEmptyState(): string {
    return `
        <div class="panel-empty pulse-empty">
            <p>No scheduled jobs yet</p>
            <p class="pulse-hint">Add schedules to ATS code blocks to see them here</p>
        </div>
    `;
}

/**
 * Render a single scheduled job card
 */
export function renderJobCard(job: ScheduledJobResponse, state: PulsePanelState): string {
    const isActive = job.state === 'active';
    const nextRun = formatRelativeTime(job.next_run_at);
    const lastRun = job.last_run_at ? formatRelativeTime(job.last_run_at) : 'Never';
    const isExpanded = state.expandedJobs.has(job.id);
    const expandIcon = isExpanded ? '▼' : '▶';

    // Prose location link if job was created from a prose document
    const proseLocationHtml = job.created_from_doc ? `
        <div class="pulse-meta-row pulse-prose-location">
            <span class="pulse-meta-label">Source:</span>
            <a href="#" class="pulse-prose-link" data-doc-id="${escapeHtml(job.created_from_doc)}" title="Open in prose editor">
                ▣ ${escapeHtml(job.created_from_doc)}
            </a>
        </div>
    ` : '';

    // Inline execution history (when expanded)
    const executionHistoryHtml = isExpanded ? renderExecutionHistory(job, state) : '';

    return `
        <div class="panel-card pulse-job-card ${isExpanded ? 'expanded' : ''}" data-job-id="${job.id}">
            <div class="panel-flex-between pulse-job-header">
                <button class="pulse-expand-toggle" data-action="toggle-expand" title="${isExpanded ? 'Collapse' : 'Expand'}">
                    ${expandIcon}
                </button>
                <div class="panel-badge-icon pulse-job-badge pulse-badge-${job.state}">
                    <span class="pulse-icon">${Pulse}</span>
                    <span class="pulse-state">${job.state}</span>
                </div>
                <div class="pulse-job-interval">${formatInterval(job.interval_seconds ?? 0)}</div>
            </div>

            <div class="pulse-job-code">
                <code>${escapeHtml(job.ats_code)}</code>
            </div>

            <div class="pulse-job-meta">
                <div class="pulse-meta-row">
                    <span class="pulse-meta-label">Next run:</span>
                    <span class="pulse-meta-value">${nextRun}</span>
                </div>
                <div class="pulse-meta-row">
                    <span class="pulse-meta-label">Last run:</span>
                    <span class="pulse-meta-value">${lastRun}</span>
                </div>
                ${proseLocationHtml}
            </div>

            <div class="pulse-job-actions">
                <button class="pulse-btn pulse-btn-force"
                        data-action="force-trigger"
                        title="Force immediate execution (bypasses deduplication)">
                    Force Trigger
                </button>
                <button class="pulse-btn pulse-btn-${isActive ? 'pause' : 'resume'}"
                        data-action="${isActive ? 'pause' : 'resume'}"
                        title="${isActive ? 'Pause job' : 'Resume job'}">
                    ${isActive ? 'Pause' : 'Resume'}
                </button>
                <button class="pulse-btn pulse-btn-delete"
                        data-action="delete"
                        title="Delete job">
                    Delete
                </button>
            </div>

            ${executionHistoryHtml}
        </div>
    `;
}

/**
 * Render execution history section for an expanded job
 */
function renderExecutionHistory(job: ScheduledJobResponse, state: PulsePanelState): string {
    const executions = state.jobExecutions.get(job.id) || [];
    const isLoading = state.loadingExecutions.has(job.id);
    const error = state.executionErrors.get(job.id);
    const limit = state.executionLimits.get(job.id) || 5;

    if (isLoading) {
        return `
            <div class="pulse-execution-history">
                <div class="pulse-execution-loading">Loading execution history...</div>
            </div>
        `;
    }

    // Show error state if fetch failed
    if (error) {
        return `
            <div class="pulse-execution-history">
                <div class="pulse-execution-error">
                    <strong>Failed to load execution history:</strong> ${error}
                    <button class="pulse-retry-button" data-action="retry-executions" data-job-id="${job.id}">
                        Retry
                    </button>
                </div>
                <a href="#" class="pulse-detailed-link" data-action="view-detailed">View detailed history →</a>
            </div>
        `;
    }

    if (executions.length === 0) {
        return `
            <div class="pulse-execution-history">
                <div class="pulse-execution-empty">
                    No executions yet. Click "Force Trigger" to run immediately.
                </div>
                <a href="#" class="pulse-detailed-link" data-action="view-detailed">View detailed history →</a>
            </div>
        `;
    }

    const executionsToShow = executions.slice(0, limit);
    const hasMore = executions.length > limit;

    return `
        <div class="pulse-execution-history">
            <div class="pulse-execution-header">
                <h4>Recent Executions (${executions.length} total)</h4>
                <a href="#" class="pulse-detailed-link" data-action="view-detailed">View detailed history →</a>
            </div>
            <div class="pulse-execution-list">
                ${executionsToShow.map(exec => renderExecutionCard(exec)).join('')}
            </div>
            ${hasMore ? `
                <button class="pulse-load-more" data-action="load-more">
                    Load 10 more executions
                </button>
            ` : ''}
        </div>
    `;
}

/**
 * Render a single execution card
 */
function renderExecutionCard(exec: Execution): string {
    const statusClass = exec.status === 'completed' ? 'success' : exec.status === 'failed' ? 'error' : 'running';
    const duration = exec.duration_ms ? formatDuration(exec.duration_ms) : '—';
    const timeAgo = formatRelativeTime(exec.started_at);

    return `
        <div class="pulse-execution-card pulse-exec-${statusClass}">
            <div class="pulse-exec-status">${exec.status.toUpperCase()}</div>
            <div class="pulse-exec-time">${timeAgo}</div>
            <div class="pulse-exec-duration">${duration}</div>
            ${exec.result_summary ? `
                <div class="pulse-exec-summary">${escapeHtml(exec.result_summary)}</div>
            ` : ''}
            ${exec.error_message ? `
                <div class="pulse-exec-error">Error: ${escapeHtml(exec.error_message)}</div>
            ` : ''}
        </div>
    `;
}

