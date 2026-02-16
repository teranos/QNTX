/**
 * Active Queue Section - Currently running async jobs
 *
 * Shows three zones:
 * 1. Active jobs (running, queued, paused)
 * 2. Recently finished jobs (completed/failed within last 8s, fading out)
 * 3. Persistent "last completed" summary row
 */

import { formatRelativeTime, escapeHtml, formatDuration } from '../html-utils.ts';
import { log, SEG } from '../logger';
import { handleError } from '../error-handler';
import { apiFetch } from '../api.ts';

/** How long recently-finished jobs stay visible (ms) */
const RECENT_WINDOW_MS = 8000;

export interface ActiveQueueResult {
    active: any[];
    recentlyFinished: any[];
    lastCompleted: any | null;
    hasRecent: boolean;
}

/**
 * Render Active Queue section
 */
export function renderActiveQueue(result: ActiveQueueResult): string {
    const { active, recentlyFinished, lastCompleted } = result;
    const hasContent = active.length > 0 || recentlyFinished.length > 0 || lastCompleted;

    if (!hasContent) {
        return `
            <div class="pulse-empty-state">
                <p>No active jobs</p>
            </div>
        `;
    }

    const activeHtml = active.length > 0
        ? `<div class="pulse-active-jobs">${active.map(job => renderActiveJob(job)).join('')}</div>`
        : '';

    const recentHtml = recentlyFinished.length > 0
        ? `<div class="pulse-active-jobs">${recentlyFinished.map(job => renderFinishedJob(job)).join('')}</div>`
        : '';

    const lastHtml = lastCompleted ? renderLastCompleted(lastCompleted) : '';

    return activeHtml + recentHtml + lastHtml;
}

/**
 * Render a single active job
 */
function renderActiveJob(job: any): string {
    const timeAgo = formatRelativeTime(job.created_at);
    const completed = job.metadata?.completed_operations || 0;
    const total = job.metadata?.total_operations || 0;

    return `
        <div class="pulse-active-job" data-job-id="${job.id}">
            <div class="pulse-job-header">
                <span class="pulse-job-status ${job.status}">${job.status}</span>
                <span class="pulse-job-time">${timeAgo}</span>
            </div>
            <div class="pulse-job-command">${escapeHtml(job.handler_name || job.metadata?.command || job.type || 'Unknown')}</div>
            ${total > 0 ? `
                <div class="pulse-job-operations">
                    ${completed} / ${total} operations
                </div>
            ` : ''}
            ${job.cost_usd ? `
                <div class="pulse-job-cost">Cost: $${job.cost_usd.toFixed(3)}</div>
            ` : ''}
        </div>
    `;
}

/**
 * Render a recently finished job (with fade-out class)
 */
function renderFinishedJob(job: any): string {
    const command = escapeHtml(job.handler_name || job.metadata?.command || job.type || 'Unknown');
    const duration = job.duration_ms ? formatDuration(job.duration_ms) : '';
    const timeAgo = job.completed_at ? formatRelativeTime(job.completed_at) : '';

    return `
        <div class="pulse-active-job pulse-job-finished" data-job-id="${job.id}">
            <div class="pulse-job-header">
                <span class="pulse-job-status ${job.status}">${job.status}</span>
                <span class="pulse-job-time">${timeAgo}</span>
            </div>
            <div class="pulse-job-command">${command}</div>
            ${duration ? `<div class="pulse-job-duration">${duration}</div>` : ''}
        </div>
    `;
}

/**
 * Render persistent last-completed summary row
 */
function renderLastCompleted(job: any): string {
    const command = escapeHtml(job.handler_name || job.metadata?.command || job.type || 'Unknown');
    const timeAgo = job.completed_at ? formatRelativeTime(job.completed_at) : '';
    const duration = job.duration_ms ? ` (${formatDuration(job.duration_ms)})` : '';

    return `
        <div class="pulse-last-completed">
            <span class="pulse-last-completed-label">Last:</span>
            <span class="pulse-last-completed-status ${job.status}">${job.status}</span>
            <span class="pulse-last-completed-command">${command}</span>
            <span class="pulse-last-completed-time">${timeAgo}${duration}</span>
        </div>
    `;
}

/**
 * Fetch active jobs from API, categorized into active, recently finished, and last completed
 */
export async function fetchActiveJobs(): Promise<ActiveQueueResult> {
    try {
        const response = await apiFetch('/api/pulse/jobs?limit=50');
        if (!response.ok) {
            log.error(SEG.PULSE, 'Failed to fetch active jobs:', response.statusText);
            return { active: [], recentlyFinished: [], lastCompleted: null, hasRecent: false };
        }

        const data = await response.json();
        const jobs = data.jobs || [];
        const now = Date.now();

        const active: any[] = [];
        const recentlyFinished: any[] = [];
        let lastCompleted: any | null = null;

        for (const job of jobs) {
            if (job.status === 'running' || job.status === 'queued' || job.status === 'paused') {
                active.push(job);
            } else if (job.status === 'completed' || job.status === 'failed') {
                // Track the most recent completed/failed job
                if (!lastCompleted || (job.completed_at && (!lastCompleted.completed_at ||
                    new Date(job.completed_at).getTime() > new Date(lastCompleted.completed_at).getTime()))) {
                    lastCompleted = job;
                }

                // Include in recently-finished if within the window
                if (job.completed_at) {
                    const completedAt = new Date(job.completed_at).getTime();
                    if (now - completedAt < RECENT_WINDOW_MS) {
                        recentlyFinished.push(job);
                    }
                }
            }
        }

        // Sort recently finished by completion time (newest first)
        recentlyFinished.sort((a, b) =>
            new Date(b.completed_at).getTime() - new Date(a.completed_at).getTime()
        );

        return {
            active,
            recentlyFinished,
            lastCompleted,
            hasRecent: recentlyFinished.length > 0,
        };
    } catch (error: unknown) {
        handleError(error, 'Error fetching active jobs', { context: SEG.PULSE, silent: true });
        return { active: [], recentlyFinished: [], lastCompleted: null, hasRecent: false };
    }
}
