/**
 * Pulse Panel Template and Utilities
 *
 * Contains the main panel template and shared utility functions.
 * Job card rendering is in schedules.ts, system status in system-status.ts,
 * and active queue in active-queue.ts.
 */

import { Pulse } from '@generated/sym.js';
import { escapeHtml as escapeHtmlUtil, formatRelativeTime as formatRelativeTimeUtil } from '../html-utils.ts';

// Time conversion constants
const MS_PER_SECOND = 1000;
const SECONDS_PER_MINUTE = 60;

/**
 * Render the main panel template (header + content wrapper)
 */
export function renderPanelTemplate(): string {
    return `
        <div class="panel-header pulse-panel-header">
            <h2 class="panel-title"><span class="pulse-icon">${Pulse}</span> Pulse</h2>
            <button class="panel-close" aria-label="Close">✕</button>
        </div>
        <div class="panel-content pulse-panel-content">
            <!-- System Status Section -->
            <div class="pulse-section pulse-system-status">
                <h3 class="pulse-section-title">System Status</h3>
                <div id="pulse-system-status-content" class="pulse-section-content">
                    <div class="panel-loading">Loading system status...</div>
                </div>
            </div>

            <!-- Active Queue Section -->
            <div class="pulse-section pulse-active-queue">
                <h3 class="pulse-section-title">Active Queue</h3>
                <div id="pulse-active-queue-content" class="pulse-section-content">
                    <div class="panel-loading">Loading active jobs...</div>
                </div>
            </div>

            <!-- Schedules Section -->
            <div class="pulse-section pulse-schedules">
                <h3 class="pulse-section-title">Schedules</h3>
                <div id="pulse-schedules-content" class="pulse-section-content">
                    <div class="panel-loading">Loading scheduled jobs...</div>
                </div>
            </div>
        </div>
    `;
}

/**
 * Format duration in milliseconds to human-readable string
 */
export function formatDuration(durationMs: number): string {
    if (durationMs < MS_PER_SECOND) return `${durationMs}ms`;
    const seconds = Math.floor(durationMs / MS_PER_SECOND);
    if (seconds < SECONDS_PER_MINUTE) return `${seconds}s`;
    const minutes = Math.floor(seconds / SECONDS_PER_MINUTE);
    const remainingSeconds = seconds % SECONDS_PER_MINUTE;
    return `${minutes}m ${remainingSeconds}s`;
}

/**
 * Format timestamp to relative time string (e.g., "5m ago", "2h from now")
 */
export const formatRelativeTime = formatRelativeTimeUtil;

/**
 * Escape HTML special characters
 */
export const escapeHtml = escapeHtmlUtil;
