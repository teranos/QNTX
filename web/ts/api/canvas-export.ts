/**
 * Canvas DOM Export
 *
 * Captures the rendered canvas workspace DOM and exports it as static HTML.
 * Only available in demo mode (QNTX_DEMO=1).
 */

import { log, SEG } from '../logger';
import { apiFetch } from '../api';

/**
 * Export canvas workspace as static HTML by capturing rendered DOM.
 * Sends to backend which writes to docs/demo/index.html.
 */
export async function exportCanvasDOM(workspace: HTMLElement): Promise<void> {
    // Capture all stylesheets
    const styleSheets = Array.from(document.styleSheets);
    let css = '';
    for (const sheet of styleSheets) {
        try {
            const rules = Array.from(sheet.cssRules);
            css += rules.map(rule => rule.cssText).join('\n');
        } catch (e) {
            // Skip cross-origin stylesheets
            log.debug(SEG.GLYPH, '[Canvas] Skipping cross-origin stylesheet');
        }
    }

    // Get workspace HTML
    const workspaceHTML = workspace.outerHTML;

    // Build complete HTML document with pan/zoom (extracted from canvas-pan.ts)
    const html = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, user-scalable=no">
<title>QNTX Canvas Export</title>
<style>
${css}
/* Critical layout overrides (must come after captured CSS for cascade priority) */
* { margin: 0; padding: 0; box-sizing: border-box; }
html, body { width: 100%; height: 100%; overflow: hidden; }
body { display: flex !important; flex-direction: column !important; }
</style>
<script>
// Canvas pan/zoom (extracted from canvas-pan.ts, dependencies stripped)
(function() {
    const ZOOM_MIN = 0.25;
    const ZOOM_MAX = 4.0;
    const ZOOM_SPEED = 0.001;

    const state = {
        panX: 0,
        panY: 0,
        scale: 1.0,
        isPanning: false,
        isPinching: false,
        startX: 0,
        startY: 0,
        startPanX: 0,
        startPanY: 0,
        startDistance: 0,
        startScale: 1.0
    };

    let touchIdentifier = null;
    let container = null;
    let contentLayer = null;

    function applyTransform() {
        if (contentLayer) {
            contentLayer.style.transform = \`translate(\${state.panX}px, \${state.panY}px) scale(\${state.scale})\`;
        }
    }

    document.addEventListener('DOMContentLoaded', function() {
        container = document.querySelector('.canvas-workspace');
        contentLayer = document.querySelector('.canvas-content-layer');
        if (!container || !contentLayer) return;

        // Desktop: wheel (two-finger scroll = pan, ctrl+wheel = zoom)
        container.addEventListener('wheel', (e) => {
            e.preventDefault();

            if (e.ctrlKey || e.metaKey) {
                // Pinch zoom
                const delta = -e.deltaY * ZOOM_SPEED;
                const oldScale = state.scale;
                const newScale = Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, oldScale * (1 + delta)));

                const rect = container.getBoundingClientRect();
                const cursorX = e.clientX - rect.left;
                const cursorY = e.clientY - rect.top;

                const scaleFactor = newScale / oldScale;
                state.panX = cursorX - (cursorX - state.panX) * scaleFactor;
                state.panY = cursorY - (cursorY - state.panY) * scaleFactor;
                state.scale = newScale;

                applyTransform();
            } else {
                // Two-finger scroll = pan
                state.panX -= e.deltaX;
                state.panY -= e.deltaY;
                applyTransform();
            }
        }, { passive: false });

        // Desktop: middle mouse button drag
        container.addEventListener('mousedown', (e) => {
            if (e.button !== 1) return;
            e.preventDefault();
            state.isPanning = true;
            state.startX = e.clientX;
            state.startY = e.clientY;
            state.startPanX = state.panX;
            state.startPanY = state.panY;
            container.style.cursor = 'grabbing';
        });

        document.addEventListener('mousemove', (e) => {
            if (!state.isPanning || touchIdentifier !== null) return;
            const deltaX = e.clientX - state.startX;
            const deltaY = e.clientY - state.startY;
            state.panX = state.startPanX + deltaX;
            state.panY = state.startPanY + deltaY;
            applyTransform();
        });

        document.addEventListener('mouseup', (e) => {
            if (!state.isPanning || touchIdentifier !== null) return;
            if (e.button !== 1) return;
            state.isPanning = false;
            container.style.cursor = '';
        });

        // Touch: single-finger pan, two-finger pinch
        container.addEventListener('touchstart', (e) => {
            if (e.touches.length === 2) {
                // Two-finger pinch zoom
                const touch1 = e.touches[0];
                const touch2 = e.touches[1];

                state.isPinching = true;
                state.isPanning = false;
                touchIdentifier = null;

                state.startDistance = Math.hypot(
                    touch2.clientX - touch1.clientX,
                    touch2.clientY - touch1.clientY
                );
                state.startScale = state.scale;
                state.startX = (touch1.clientX + touch2.clientX) / 2;
                state.startY = (touch1.clientY + touch2.clientY) / 2;
            } else if (e.touches.length === 1) {
                // Single touch pan
                const touch = e.touches[0];
                touchIdentifier = touch.identifier;
                state.isPanning = true;
                state.isPinching = false;
                state.startX = touch.clientX;
                state.startY = touch.clientY;
                state.startPanX = state.panX;
                state.startPanY = state.panY;
            }
        }, { passive: true });

        container.addEventListener('touchmove', (e) => {
            if (state.isPinching && e.touches.length === 2) {
                e.preventDefault();

                const touch1 = e.touches[0];
                const touch2 = e.touches[1];

                const currentDistance = Math.hypot(
                    touch2.clientX - touch1.clientX,
                    touch2.clientY - touch1.clientY
                );

                const scaleChange = currentDistance / state.startDistance;
                const oldScale = state.startScale;
                const newScale = Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, oldScale * scaleChange));

                const rect = container.getBoundingClientRect();
                const centerX = (touch1.clientX + touch2.clientX) / 2 - rect.left;
                const centerY = (touch1.clientY + touch2.clientY) / 2 - rect.top;

                const scaleFactor = newScale / oldScale;
                state.panX = centerX - (centerX - state.panX) * scaleFactor;
                state.panY = centerY - (centerY - state.panY) * scaleFactor;
                state.scale = newScale;

                applyTransform();
            } else if (state.isPanning && touchIdentifier !== null) {
                const touch = Array.from(e.touches).find(t => t.identifier === touchIdentifier);
                if (!touch) return;

                e.preventDefault();

                const deltaX = touch.clientX - state.startX;
                const deltaY = touch.clientY - state.startY;

                state.panX = state.startPanX + deltaX;
                state.panY = state.startPanY + deltaY;

                applyTransform();
            }
        }, { passive: false });

        container.addEventListener('touchend', (e) => {
            if (state.isPinching) {
                if (e.touches.length < 2) {
                    state.isPinching = false;
                }
            } else if (state.isPanning && touchIdentifier !== null) {
                const ended = Array.from(e.changedTouches).some(t => t.identifier === touchIdentifier);
                if (!ended) return;
                state.isPanning = false;
                touchIdentifier = null;
            }
        });
    });
})();
</script>
</head>
<body>
${workspaceHTML}
</body>
</html>`;

    // Send to backend
    const response = await apiFetch('/api/canvas/export-dom', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ html }),
    });

    if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error || 'Export failed');
    }

    const result = await response.json();
    log.info(SEG.GLYPH, `[Canvas] Exported to ${result.path}`);
}
