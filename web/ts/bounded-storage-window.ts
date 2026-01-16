/**
 * Bounded Storage Window
 *
 * Displays bounded storage configuration, limits, and recent events.
 * This is NOT a toast - it's a persistent monitoring window for storage state.
 *
 * Features:
 * - Current fill status per actor/context
 * - Recent eviction history
 * - Configuration summary
 * - Visual status indicator (green/yellow/red)
 */

import { Window } from './components/window.ts';
import { sendMessage } from './websocket.ts';
import { DB } from '@generated/sym.js';
import { tooltip } from './components/tooltip.ts';
import { formatRelativeTime } from './html-utils.ts';
import type { StorageWarningMessage, StorageEvictionMessage } from '../types/websocket';

/**
 * Storage bucket status (per actor/context combination)
 */
interface StorageBucket {
    actor: string;
    context: string;
    current: number;
    limit: number;
    fillPercent: number;
    timeUntilFull: string;
    lastUpdated: number;
}

/**
 * Eviction event record
 */
interface EvictionEvent {
    actor: string;
    context: string;
    deletionsCount: number;
    message: string;
    eventType: string;
    timestamp: number;
}

/**
 * Storage status level
 */
type StatusLevel = 'healthy' | 'warning' | 'critical';

/** localStorage key for persisting evictions */
const EVICTIONS_STORAGE_KEY = 'qntx-bounded-storage-evictions';

/** How long to keep evictions in storage (7 days) */
const EVICTION_RETENTION_MS = 7 * 24 * 60 * 60 * 1000;

/** How long to show recent evictions in ticker (3 minutes) */
const RECENT_EVICTION_THRESHOLD_MS = 3 * 60 * 1000;

class BoundedStorageWindow {
    private window: Window;
    private buckets: Map<string, StorageBucket> = new Map();
    private evictions: EvictionEvent[] = [];
    private maxEvictions = 50; // Keep last 50 eviction events
    private tooltipCleanup: (() => void) | null = null;

    constructor() {
        this.window = new Window({
            id: 'bounded-storage-window',
            title: `${DB} Bounded Storage`,
            width: '500px',
            height: 'auto',
            onShow: () => this.onShow(),
            onHide: () => this.onHide()
        });

        // Load persisted evictions from localStorage
        this.loadEvictionsFromStorage();

        this.render();
    }

    /**
     * Load evictions from localStorage
     * Prunes entries older than retention period
     */
    private loadEvictionsFromStorage(): void {
        try {
            const stored = localStorage.getItem(EVICTIONS_STORAGE_KEY);
            if (!stored) return;

            const parsed: EvictionEvent[] = JSON.parse(stored);
            const cutoff = Date.now() - EVICTION_RETENTION_MS;

            // Filter out old evictions and limit to maxEvictions
            this.evictions = parsed
                .filter(e => e.timestamp > cutoff)
                .slice(0, this.maxEvictions);

            // If we pruned anything, save the cleaned list back
            if (this.evictions.length < parsed.length) {
                this.saveEvictionsToStorage();
            }
        } catch (e) {
            // Invalid data, clear it
            localStorage.removeItem(EVICTIONS_STORAGE_KEY);
        }
    }

    /**
     * Save evictions to localStorage
     */
    private saveEvictionsToStorage(): void {
        try {
            localStorage.setItem(EVICTIONS_STORAGE_KEY, JSON.stringify(this.evictions));
        } catch (e) {
            // Storage full or unavailable - continue without persistence
        }
    }

    private onShow(): void {
        // Request current bounded storage status from server
        sendMessage({ type: 'get_bounded_storage_status' });
    }

    private onHide(): void {
        if (this.tooltipCleanup) {
            this.tooltipCleanup();
            this.tooltipCleanup = null;
        }
    }

    /**
     * Toggle window visibility
     */
    toggle(): void {
        this.window.toggle();
    }

    /**
     * Show window
     */
    show(): void {
        this.window.show();
    }

    /**
     * Handle storage warning - update bucket status
     */
    handleWarning(data: StorageWarningMessage): void {
        const key = `${data.actor}:${data.context}`;

        this.buckets.set(key, {
            actor: data.actor,
            context: data.context,
            current: data.current,
            limit: data.limit,
            fillPercent: data.fill_percent,
            timeUntilFull: data.time_until_full,
            lastUpdated: data.timestamp || Date.now()
        });

        // Re-render if window is visible
        if (this.window.isVisible()) {
            this.render();
        }

        // Update status indicator
        this.updateStatusIndicator();
    }

    /**
     * Handle storage eviction - add to history
     */
    handleEviction(data: StorageEvictionMessage): void {
        this.evictions.unshift({
            actor: data.actor,
            context: data.context,
            deletionsCount: data.deletions_count,
            message: data.message,
            eventType: data.event_type,
            timestamp: Date.now()
        });

        // Trim old events
        if (this.evictions.length > this.maxEvictions) {
            this.evictions = this.evictions.slice(0, this.maxEvictions);
        }

        // Persist to localStorage
        this.saveEvictionsToStorage();

        // Re-render if window is visible
        if (this.window.isVisible()) {
            this.render();
        }

        // Update status indicator
        this.updateStatusIndicator();

        // Notify listeners of eviction update
        this.notifyEvictionUpdate();
    }

    /**
     * Get overall status level
     */
    getStatusLevel(): StatusLevel {
        let maxFill = 0;

        for (const bucket of this.buckets.values()) {
            if (bucket.fillPercent > maxFill) {
                maxFill = bucket.fillPercent;
            }
        }

        if (maxFill >= 0.9) return 'critical';
        if (maxFill >= 0.7) return 'warning';
        return 'healthy';
    }

    /**
     * Update external status indicator (if present)
     */
    private updateStatusIndicator(): void {
        const indicator = document.getElementById('bounded-storage-indicator');
        if (!indicator) return;

        const level = this.getStatusLevel();
        indicator.setAttribute('data-status', level);

        // Update tooltip
        const bucketCount = this.buckets.size;
        const recentEvictions = this.evictions.filter(
            e => Date.now() - e.timestamp < 3600000 // Last hour
        ).length;

        let tooltipText = `Bounded Storage: ${bucketCount} active bucket(s)`;
        if (recentEvictions > 0) {
            tooltipText += `\n${recentEvictions} eviction(s) in last hour`;
        }
        indicator.setAttribute('data-tooltip', tooltipText);
    }

    private render(): void {
        const content = this.window.getContentElement();

        // Build content
        let html = '<div class="bounded-storage-content">';

        // Status summary
        const level = this.getStatusLevel();
        html += `
            <div class="bounded-storage-summary bounded-storage-${level}">
                <span class="bounded-storage-status-icon">${this.getStatusIcon(level)}</span>
                <span class="bounded-storage-status-text">${this.getStatusText(level)}</span>
            </div>
        `;

        // Weekly eviction chart (if there are evictions)
        const weeklyData = this.getWeeklyEvictionsByDay();
        const totalWeekly = weeklyData.reduce((sum, d) => sum + d.count, 0);

        if (totalWeekly > 0) {
            const maxCount = Math.max(...weeklyData.map(d => d.count), 1);
            html += '<div class="bounded-storage-section">';
            html += '<h4 class="bounded-storage-section-title">Evictions This Week</h4>';
            html += '<div class="bounded-storage-chart">';
            html += '<div class="bounded-storage-chart-bars">';

            for (const day of weeklyData) {
                const heightPct = Math.round((day.count / maxCount) * 100);
                const dayLabel = day.label;
                html += `
                    <div class="bounded-storage-chart-bar-container has-tooltip"
                         data-tooltip="${day.count} attestations evicted on ${day.fullLabel}">
                        <div class="bounded-storage-chart-bar" style="height: ${heightPct}%"></div>
                        <span class="bounded-storage-chart-label">${dayLabel}</span>
                    </div>
                `;
            }

            html += '</div>';
            html += `<div class="bounded-storage-chart-total">${totalWeekly.toLocaleString()} total evicted</div>`;
            html += '</div>';
            html += '</div>';
        }

        // Active buckets
        html += '<div class="bounded-storage-section">';
        html += '<h4 class="bounded-storage-section-title">Storage Buckets</h4>';

        if (this.buckets.size === 0) {
            html += '<p class="bounded-storage-empty">No bounded storage limits configured or no data received yet.</p>';
        } else {
            html += '<div class="bounded-storage-buckets">';
            for (const bucket of this.buckets.values()) {
                const fillClass = this.getFillClass(bucket.fillPercent);
                const fillPct = Math.round(bucket.fillPercent * 100);

                html += `
                    <div class="bounded-storage-bucket">
                        <div class="bounded-storage-bucket-header">
                            <span class="bounded-storage-bucket-name">${this.escapeHtml(bucket.actor)}/${this.escapeHtml(bucket.context)}</span>
                            <span class="bounded-storage-bucket-count">${bucket.current.toLocaleString()} / ${bucket.limit.toLocaleString()}</span>
                        </div>
                        <div class="bounded-storage-bucket-bar">
                            <div class="bounded-storage-bucket-fill ${fillClass}" style="width: ${fillPct}%"></div>
                        </div>
                        <div class="bounded-storage-bucket-footer">
                            <span class="bounded-storage-bucket-pct">${fillPct}% full</span>
                            ${bucket.timeUntilFull ? `<span class="bounded-storage-bucket-time has-tooltip" data-tooltip="Estimated time until limit is reached">~${bucket.timeUntilFull} until full</span>` : ''}
                        </div>
                    </div>
                `;
            }
            html += '</div>';
        }
        html += '</div>';

        // Recent evictions
        html += '<div class="bounded-storage-section">';
        html += '<h4 class="bounded-storage-section-title">Recent Evictions</h4>';

        if (this.evictions.length === 0) {
            html += '<p class="bounded-storage-empty">No evictions recorded.</p>';
        } else {
            html += '<div class="bounded-storage-evictions">';
            // Show last 10 evictions
            const recentEvictions = this.evictions.slice(0, 10);
            for (const eviction of recentEvictions) {
                const timeAgo = formatRelativeTime(new Date(eviction.timestamp).toISOString());
                const eventTypeLabel = this.formatEventType(eviction.eventType);
                html += `
                    <div class="bounded-storage-eviction">
                        <div class="bounded-storage-eviction-header">
                            <span class="bounded-storage-eviction-icon">⚠</span>
                            <span class="bounded-storage-eviction-count">${eviction.deletionsCount} attestation(s) evicted</span>
                            ${eventTypeLabel ? `<span class="bounded-storage-eviction-type has-tooltip" data-tooltip="Eviction strategy: ${eviction.eventType}">${eventTypeLabel}</span>` : ''}
                            <span class="bounded-storage-eviction-time">${timeAgo}</span>
                        </div>
                        <div class="bounded-storage-eviction-detail">
                            ${this.escapeHtml(eviction.actor)}/${this.escapeHtml(eviction.context)}
                        </div>
                    </div>
                `;
            }
            html += '</div>';

            if (this.evictions.length > 10) {
                html += `<p class="bounded-storage-more">+ ${this.evictions.length - 10} more evictions</p>`;
            }
        }
        html += '</div>';

        html += '</div>';

        content.innerHTML = html;

        // Setup tooltips
        this.setupTooltips();
    }

    private setupTooltips(): void {
        if (this.tooltipCleanup) {
            this.tooltipCleanup();
        }
        const content = this.window.getContentElement();
        this.tooltipCleanup = tooltip.attach(content, '.has-tooltip');
    }

    private getStatusIcon(level: StatusLevel): string {
        switch (level) {
            case 'healthy': return '●';
            case 'warning': return '◐';
            case 'critical': return '◉';
        }
    }

    private getStatusText(level: StatusLevel): string {
        switch (level) {
            case 'healthy': return 'Storage healthy';
            case 'warning': return 'Storage approaching limits';
            case 'critical': return 'Storage near capacity';
        }
    }

    private getFillClass(fillPercent: number): string {
        if (fillPercent >= 0.9) return 'bounded-storage-fill-critical';
        if (fillPercent >= 0.7) return 'bounded-storage-fill-warning';
        return 'bounded-storage-fill-healthy';
    }

    private escapeHtml(text: string): string {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    /**
     * Format eviction event type for display
     * Returns a short, human-readable label
     */
    private formatEventType(eventType: string): string {
        const labels: Record<string, string> = {
            'fifo': 'FIFO',
            'lru': 'LRU',
            'priority': 'Priority',
            'ttl': 'TTL',
            'manual': 'Manual',
            'limit': 'Limit',
            'budget': 'Budget',
        };

        return labels[eventType.toLowerCase()] || eventType.toUpperCase();
    }

    /**
     * Check if there are any active warnings or recent evictions
     */
    hasActiveIssues(): boolean {
        // Any bucket over 70% or any eviction in last hour
        for (const bucket of this.buckets.values()) {
            if (bucket.fillPercent >= 0.7) return true;
        }

        const oneHourAgo = Date.now() - 3600000;
        return this.evictions.some(e => e.timestamp > oneHourAgo);
    }

    /**
     * Get bucket count
     */
    getBucketCount(): number {
        return this.buckets.size;
    }

    /**
     * Get recent eviction count (last hour)
     */
    getRecentEvictionCount(): number {
        const oneHourAgo = Date.now() - 3600000;
        return this.evictions.filter(e => e.timestamp > oneHourAgo).length;
    }

    /**
     * Get weekly eviction count (last 7 days)
     */
    getWeeklyEvictionCount(): number {
        const oneWeekAgo = Date.now() - (7 * 24 * 3600000);
        return this.evictions.filter(e => e.timestamp > oneWeekAgo).length;
    }

    /**
     * Get evictions grouped by day for the last 7 days
     * Returns array of { date, count, label, fullLabel } objects
     */
    getWeeklyEvictionsByDay(): Array<{ date: Date; count: number; label: string; fullLabel: string }> {
        const days: Array<{ date: Date; count: number; label: string; fullLabel: string }> = [];
        const dayNames = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];
        const now = new Date();

        // Create 7 day buckets
        for (let i = 6; i >= 0; i--) {
            const date = new Date(now);
            date.setDate(date.getDate() - i);
            date.setHours(0, 0, 0, 0);

            const dayOfWeek = dayNames[date.getDay()];
            const fullLabel = date.toLocaleDateString('en-US', {
                weekday: 'short',
                month: 'short',
                day: 'numeric'
            });

            days.push({
                date,
                count: 0,
                label: i === 0 ? 'Today' : i === 1 ? 'Yest' : dayOfWeek,
                fullLabel
            });
        }

        // Count evictions per day
        for (const eviction of this.evictions) {
            const evictionDate = new Date(eviction.timestamp);
            evictionDate.setHours(0, 0, 0, 0);

            for (const day of days) {
                if (evictionDate.getTime() === day.date.getTime()) {
                    day.count += eviction.deletionsCount;
                    break;
                }
            }
        }

        return days;
    }

    /**
     * Get total evicted attestations in last week
     */
    getWeeklyEvictedAttestations(): number {
        const oneWeekAgo = Date.now() - (7 * 24 * 3600000);
        return this.evictions
            .filter(e => e.timestamp > oneWeekAgo)
            .reduce((sum, e) => sum + e.deletionsCount, 0);
    }

    /**
     * Get the most recent eviction within the ticker threshold (3 minutes)
     * Returns null if no recent eviction
     */
    getMostRecentEviction(): EvictionEvent | null {
        if (this.evictions.length === 0) return null;

        const mostRecent = this.evictions[0];
        const age = Date.now() - mostRecent.timestamp;

        if (age <= RECENT_EVICTION_THRESHOLD_MS) {
            return mostRecent;
        }

        return null;
    }

    /**
     * Get aggregated recent evictions within the ticker threshold
     * Sums all evictions within the window for a more comprehensive view
     */
    getRecentEvictionsSummary(): { count: number; totalDeleted: number; mostRecentTimestamp: number } | null {
        const cutoff = Date.now() - RECENT_EVICTION_THRESHOLD_MS;
        const recentEvictions = this.evictions.filter(e => e.timestamp > cutoff);

        if (recentEvictions.length === 0) return null;

        return {
            count: recentEvictions.length,
            totalDeleted: recentEvictions.reduce((sum, e) => sum + e.deletionsCount, 0),
            mostRecentTimestamp: recentEvictions[0].timestamp
        };
    }

    /**
     * Register a callback to be notified when eviction stats change
     */
    private evictionCallbacks: Array<() => void> = [];

    onEvictionUpdate(callback: () => void): () => void {
        this.evictionCallbacks.push(callback);
        return () => {
            const index = this.evictionCallbacks.indexOf(callback);
            if (index !== -1) {
                this.evictionCallbacks.splice(index, 1);
            }
        };
    }

    private notifyEvictionUpdate(): void {
        for (const callback of this.evictionCallbacks) {
            try {
                callback();
            } catch (e) {
                // Ignore callback errors
            }
        }
    }
}

// Export singleton instance
export const boundedStorageWindow = new BoundedStorageWindow();

/**
 * Create a status indicator element for bounded storage
 * Can be placed in status bar or other UI locations
 */
export function createBoundedStorageIndicator(): HTMLElement {
    const indicator = document.createElement('div');
    indicator.id = 'bounded-storage-indicator';
    indicator.className = 'bounded-storage-indicator has-tooltip';
    indicator.setAttribute('data-tooltip', 'Bounded Storage: Click to view status');
    indicator.setAttribute('data-status', 'healthy');
    indicator.setAttribute('role', 'button');
    indicator.setAttribute('tabindex', '0');
    indicator.setAttribute('aria-label', 'Bounded storage status');

    // Icon
    const icon = document.createElement('span');
    icon.className = 'bounded-storage-indicator-icon';
    icon.textContent = DB;
    indicator.appendChild(icon);

    // Click to open window
    indicator.addEventListener('click', () => {
        boundedStorageWindow.toggle();
    });

    // Keyboard activation
    indicator.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            boundedStorageWindow.toggle();
        }
    });

    return indicator;
}

/**
 * Format a short relative time (e.g., "20s ago", "2m ago")
 */
function formatShortRelativeTime(timestamp: number): string {
    const seconds = Math.floor((Date.now() - timestamp) / 1000);

    if (seconds < 60) {
        return `${seconds}s ago`;
    }

    const minutes = Math.floor(seconds / 60);
    return `${minutes}m ago`;
}

/**
 * Create a live eviction ticker element
 * Shows recent evictions (within 3 minutes) with auto-updating time
 *
 * Usage:
 *   const ticker = createEvictionTicker();
 *   statusBar.appendChild(ticker);
 *
 * The ticker auto-updates and hides itself when no recent evictions.
 * Call ticker.destroy() to clean up when removing from DOM.
 */
export function createEvictionTicker(): HTMLElement & { destroy: () => void } {
    const ticker = document.createElement('div');
    ticker.className = 'eviction-ticker';
    ticker.style.display = 'none'; // Hidden by default

    // Text content
    const text = document.createElement('span');
    text.className = 'eviction-ticker-text';
    ticker.appendChild(text);

    // We'll assign the destroy method before returning
    let destroyFn: () => void = () => {};

    let updateInterval: number | null = null;
    let unsubscribe: (() => void) | null = null;

    /**
     * Update the ticker display
     */
    function update(): void {
        const summary = boundedStorageWindow.getRecentEvictionsSummary();

        if (!summary) {
            ticker.style.display = 'none';
            return;
        }

        ticker.style.display = 'flex';
        const timeAgo = formatShortRelativeTime(summary.mostRecentTimestamp);
        text.textContent = `evicted: ${summary.totalDeleted} ats, ${timeAgo}`;
    }

    // Initial update
    update();

    // Update every 10 seconds for time freshness
    updateInterval = window.setInterval(update, 10000);

    // Subscribe to eviction updates for immediate refresh
    unsubscribe = boundedStorageWindow.onEvictionUpdate(() => {
        update();
        // Pulse animation on new eviction
        ticker.classList.add('just-updated');
        setTimeout(() => ticker.classList.remove('just-updated'), 500);
    });

    /**
     * Cleanup function
     */
    destroyFn = () => {
        if (updateInterval !== null) {
            clearInterval(updateInterval);
            updateInterval = null;
        }
        if (unsubscribe) {
            unsubscribe();
            unsubscribe = null;
        }
    };

    // Return ticker with destroy method attached
    return Object.assign(ticker, { destroy: destroyFn });
}
