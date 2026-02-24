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

    // Build complete HTML document
    const html = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>QNTX Canvas Export</title>
<style>
${css}
/* Critical layout overrides (must come after captured CSS for cascade priority) */
* { margin: 0; padding: 0; box-sizing: border-box; }
html, body { width: 100%; height: 100%; overflow: hidden; }
body { display: flex !important; flex-direction: column !important; }
</style>
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
