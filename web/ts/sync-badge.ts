// Sync badge: shows pending canvas sync queue count in system drawer header.
// Hidden when queue is empty, visible with count when items are pending.

import { canvasSyncQueue } from './api/canvas-sync';

let badge: HTMLDivElement | null = null;

function update(): void {
    if (!badge) return;
    const count = canvasSyncQueue.size;
    if (count === 0) {
        badge.hidden = true;
    } else {
        badge.hidden = false;
        badge.textContent = `${count} pending`;
    }
}

export function initSyncBadge(): void {
    badge = document.createElement('div');
    badge.id = 'sync-badge';
    badge.className = 'sync-badge';
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
    }

    canvasSyncQueue.onChange(update);
    update();
}
