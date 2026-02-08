// Usage badge component for real-time cost tracking
// Clicking the badge opens the usage-chart glyph

import type { UsageUpdateMessage } from '../types/websocket';

// Internal usage stats derived from WebSocket message
interface UsageStats {
    total_cost: number;
    requests: number;
    success: number;
    tokens: number;
    models: number;
    since: string;
}

// Create badge element
export function createUsageBadge(): HTMLDivElement {
    const badge = document.createElement('div');
    badge.id = 'usage-badge';
    badge.className = 'usage-badge';
    badge.textContent = '$0.00';
    badge.title = 'Click for cost chart';

    // Open usage-chart glyph on click
    badge.addEventListener('click', () => {
        const usageGlyph = document.querySelector('[data-glyph-id="usage-chart"]') as HTMLElement;
        if (usageGlyph) {
            usageGlyph.click();
        }
    });

    // Insert at the beginning of system drawer header (leftmost position)
    const drawerHeader = document.getElementById('system-drawer-header');
    if (drawerHeader && drawerHeader.firstChild) {
        drawerHeader.insertBefore(badge, drawerHeader.firstChild);
    } else if (drawerHeader) {
        drawerHeader.appendChild(badge);
    } else {
        // Fallback: append to body if drawer header not ready yet
        document.body.appendChild(badge);
    }

    return badge;
}

// Update badge with new stats
export function updateUsageBadge(stats: UsageStats): void {
    const badge = document.getElementById('usage-badge');
    if (!badge) return;

    badge.textContent = `$${stats.total_cost.toFixed(2)}`;
}

// Initialize usage badge
export function initUsageBadge(): void {
    createUsageBadge();
}

// Handle usage update from WebSocket - accepts full message type
export function handleUsageUpdate(message: UsageUpdateMessage): void {
    // Extract usage stats from message
    const stats: UsageStats = {
        total_cost: message.total_cost,
        requests: message.requests,
        success: message.success,
        tokens: message.tokens,
        models: message.models,
        since: message.since,
    };
    updateUsageBadge(stats);
}