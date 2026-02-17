// Sync badge: shows pending canvas sync queue count in system drawer header.
// Hidden when queue is empty, visible with count when items are pending.

import { canvasSyncQueue } from './api/canvas-sync';
import { tooltip } from './components/tooltip';

let badge: HTMLDivElement | null = null;

function update(): void {
    if (!badge) return;
    const count = canvasSyncQueue.size;
    if (count === 0) {
        badge.hidden = true;
        badge.removeAttribute('data-tooltip');
    } else {
        badge.hidden = false;
        badge.textContent = `${count} pending`;

        // Color escalation by queue depth
        if (count >= 15) {
            badge.style.color = '#e05544';
        } else if (count >= 8) {
            badge.style.color = '#d48a2e';
        } else if (count >= 5) {
            badge.style.color = '#b8a44a';
        } else {
            badge.style.color = '';
        }

        const entries = canvasSyncQueue.entries;
        badge.dataset.tooltip = entries.map(e => {
            const retry = e.retryCount ? ` (retry ${e.retryCount})` : '';
            return `${e.op} ${e.id}${retry}`;
        }).join('\n');
    }
}

export function initSyncBadge(): void {
    badge = document.createElement('div');
    badge.id = 'sync-badge';
    badge.className = 'sync-badge has-tooltip';
    badge.hidden = true;

    const drawerHeader = document.getElementById('system-drawer-header');
    if (drawerHeader) {
        // Insert after usage badge (second position)
        const usageBadge = document.getElementById('usage-badge');
        if (usageBadge && usageBadge.nextSibling) {
            drawerHeader.insertBefore(badge, usageBadge.nextSibling);
        } else {
            drawerHeader.appendChild(badge);
        }

        // Attach tooltip to the badge itself (event stops at badge, won't toggle drawer)
        tooltip.attach(drawerHeader, '#sync-badge');
    }

    canvasSyncQueue.onChange(update);
    update();
}
