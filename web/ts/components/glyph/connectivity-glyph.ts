/**
 * Connectivity Glyph — surfaces raw fetch/WebSocket failures directly to the user.
 *
 * Auto-opens on the first failure. Subscribes to `connectivity.subscribeFailures`
 * and updates the failure list live. No user interaction required — load the
 * page in a broken state, the URL and reason are on screen.
 */

import { connectivity, type Failure } from '../../client';
import { log, SEG } from '../../logger';
import { glyphRun } from '@qntx/glyphs';
import type { Glyph } from '@qntx/glyphs';

const CONNECTIVITY_GLYPH_ID = 'connectivity';

function formatFailure(f: Failure): string {
    const ts = new Date(f.at).toISOString().slice(11, 19);
    return `${ts}  ${f.source}  ${f.url}\n  ${f.reason}`;
}

function renderConnectivityContent(): HTMLElement {
    const container = document.createElement('div');
    container.className = 'glyph-content';
    container.style.display = 'flex';
    container.style.flexDirection = 'column';
    container.style.gap = '8px';
    container.style.padding = '10px 12px';
    container.style.fontFamily = 'ui-monospace, SFMono-Regular, Menlo, monospace';
    container.style.fontSize = '11px';
    container.style.color = 'var(--text-on-dark)';

    const header = document.createElement('div');
    header.textContent = 'Connectivity failures';
    header.style.fontSize = '11px';
    header.style.opacity = '0.7';
    header.style.textTransform = 'uppercase';
    header.style.letterSpacing = '0.5px';

    const list = document.createElement('pre');
    list.style.margin = '0';
    // Let the widest line drive the window's intrinsic width via the
    // ResizeObserver in packages/glyphs/manifestations/window.ts. maxWidth
    // (viewport * MAX_VIEWPORT_WIDTH_RATIO) still caps very long URLs.
    list.style.whiteSpace = 'pre';
    list.style.color = '#e06060';

    function render(): void {
        const failures = connectivity.failures;
        if (failures.length === 0) {
            list.textContent = '(none)';
            list.style.color = 'var(--text-secondary)';
            return;
        }
        list.style.color = '#e06060';
        list.textContent = failures.slice().reverse().map(formatFailure).join('\n\n');
    }

    render();
    connectivity.subscribeFailures(() => render());

    container.append(header, list);
    return container;
}

/**
 * Add the connectivity glyph to the tray and open it. No-op if already present.
 * Called on the first failure event via subscribeFailures.
 */
export function spawnConnectivityGlyph(): void {
    if (glyphRun.has(CONNECTIVITY_GLYPH_ID)) {
        glyphRun.openGlyph(CONNECTIVITY_GLYPH_ID);
        return;
    }

    const glyph: Glyph = {
        id: CONNECTIVITY_GLYPH_ID,
        title: 'Connectivity',
        renderContent: renderConnectivityContent,
        initialHeight: '260px',
        onClose: () => {
            log.debug(SEG.GLYPH, '[ConnectivityGlyph] Closed');
        },
    };

    glyphRun.add(glyph);
    glyphRun.openGlyph(CONNECTIVITY_GLYPH_ID);
}
