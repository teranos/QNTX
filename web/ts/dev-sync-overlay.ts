/**
 * Dev Sync Overlay
 *
 * Injects CSS that makes canvas glyph sync state visible during development.
 * Gated behind the server's dev mode flag — zero cost in production.
 */

import { isDevMode } from './dev-mode.ts';

const STYLE_ID = 'dev-sync-overlay';

const DEV_SYNC_CSS = `
/* Dev-only: sync state indicators on canvas glyphs */
.canvas-glyph[data-sync-state="unsynced"] {
    outline: 1px dashed var(--color-warning) !important;
}
.canvas-glyph[data-sync-state="syncing"] {
    outline: 1px dashed var(--color-warning) !important;
    animation: dev-sync-pulse 0.8s ease-in-out infinite;
}
.canvas-glyph[data-sync-state="failed"] {
    outline: 1px solid var(--color-error) !important;
}
@keyframes dev-sync-pulse {
    0%, 100% { outline-color: var(--color-warning); }
    50% { outline-color: transparent; }
}
`;

export async function initDevSyncOverlay(): Promise<void> {
    if (!await isDevMode()) return;
    if (document.getElementById(STYLE_ID)) return;

    const style = document.createElement('style');
    style.id = STYLE_ID;
    style.textContent = DEV_SYNC_CSS;
    document.head.appendChild(style);
}
