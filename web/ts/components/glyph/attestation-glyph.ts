/**
 * Attestation Glyph (+) — view a single attestation on canvas
 *
 * Opened via double-click on attestation result items in AX or SE glyphs.
 * Title bar IS the triple (subjects is predicates of contexts).
 * Attributes shown below title bar only when present.
 * No attributes → compact title-bar-only glyph.
 * Metadata (actors, source, timestamps, id) hidden by default —
 * revealed via hover pill at bottom center of title bar.
 */

import type { Glyph } from '@qntx/glyphs';
import { wireExpandToWindow, teardownWindowDrag, removeWindowControls, isInWindowState, setWindowState, glyphRun } from '@qntx/glyphs';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { AS } from '@generated/sym.js';
import { renderTriple } from './attestation-triple';
import { log, SEG } from '../../logger';
import { canvasPlaced } from '@qntx/glyphs';
import { preventDrag, makeDraggable, makeResizable, storeCleanup } from '@qntx/glyphs';
import { screenToCanvas } from './canvas/canvas-pan';
import { uiState } from '../../state/ui';
import { spawnOnCanvas } from './spawn-on-canvas';
import { AZURE, AZURE_KEYWORD, AZURE_VALUE, renderAttestationAttrs, parseAttributes } from './attestation-attrs';

// Re-export for consumers that import from this file
export { extractArray, extractObject, renderItem, renderAttributeValue, renderAttestationAttrs, parseAttributes } from './attestation-attrs';
export type { FileType } from './attestation-attrs';

/**
 * Build metadata lines from attestation fields.
 */
function buildMetaLines(attestation: Attestation): string[] {
    const lines: string[] = [];
    if (attestation.actors && attestation.actors.length > 0) {
        lines.push(`actors: ${attestation.actors.join(', ')}`);
    }
    if (attestation.source) {
        const attrs = parseAttributes(attestation);
        const version = attrs && typeof attrs['source_version'] === 'string' ? attrs['source_version'] : '';
        lines.push(version ? `source: ${attestation.source} ${version}` : `source: ${attestation.source}`);
    }
    if (attestation.timestamp) {
        lines.push(`timestamp: ${formatTimestamp(attestation.timestamp)}`);
    }
    if (attestation.created_at) {
        lines.push(`created: ${formatTimestamp(attestation.created_at)}`);
    }
    if (attestation.signer_did) {
        // Cyan color for signer (between green and purple)
        lines.push(`<span style="color: #00d4aa">signer: ${attestation.signer_did}</span>`);
    }
    if (attestation.signature && attestation.signature.length > 0) {
        lines.push(`signature: ${attestation.signature.length} bytes`);
    }
    if (attestation.id) {
        lines.push(`id: ${attestation.id}`);
    }
    return lines;
}

/**
 * Create an Attestation glyph.
 * Title bar = triple. Attributes below if present. Metadata behind hover pill.
 */
export function createAttestationGlyph(glyph: Glyph): HTMLElement {
    let attestation: Attestation | null = null;
    try {
        if (glyph.content) {
            attestation = JSON.parse(glyph.content);
        }
    } catch (err) {
        log.warn(SEG.GLYPH, `[AsGlyph] Failed to parse attestation content for ${glyph.id}:`, err);
    }

    const attrs = attestation ? parseAttributes(attestation) : null;

    // Title bar: + symbol + triple + expand button + metadata pill
    const titleBar = document.createElement('div');
    titleBar.className = 'glyph-title-bar glyph-title-bar--auto';
    titleBar.style.position = 'relative';

    const symbol = document.createElement('span');
    symbol.textContent = AS;
    symbol.style.fontWeight = 'bold';
    symbol.style.flexShrink = '0';
    symbol.style.color = AZURE;
    titleBar.appendChild(symbol);

    if (attestation) {
        const tripleText = renderTriple(attestation, {
            palette: { value: AZURE_VALUE, keyword: AZURE_KEYWORD },
            showWatcherEyes: true,
        });
        titleBar.appendChild(tripleText);
    }

    // Expand/place button
    const expandBtn = document.createElement('button');
    expandBtn.className = 'titlebar-btn';
    expandBtn.textContent = '\u2B06'; // ⬆
    expandBtn.title = 'Expand to window';
    expandBtn.style.flexShrink = '0';
    expandBtn.style.marginLeft = 'auto';
    preventDrag(expandBtn);
    titleBar.appendChild(expandBtn);

    // Metadata pill — appears on hover at bottom center of title bar
    if (attestation) {
        const metaLines = buildMetaLines(attestation);
        if (metaLines.length > 0) {
            const pill = document.createElement('div');
            pill.className = 'as-meta-pill';

            const metaPopover = document.createElement('div');
            metaPopover.className = 'meta-popover as-meta-popover';
            metaPopover.innerHTML = metaLines.join('<br>');

            pill.appendChild(metaPopover);
            titleBar.appendChild(pill);
        }
    }

    // Compact when no attributes, expanded when attributes present
    const hasContent = !!attrs;

    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-attestation-glyph',
        defaults: { x: 200, y: 200, width: 420, height: hasContent ? 200 : 28 },
        resizable: hasContent,
        useMinHeight: true,
        logLabel: 'AsGlyph',
    });
    element.style.minWidth = '200px';

    element.appendChild(titleBar);

    // Attributes content — only when there are attributes to show
    if (attestation && attrs) {
        const content = document.createElement('div');
        content.className = 'glyph-content-area';
        content.style.padding = '4px 8px';
        content.style.backgroundColor = 'rgba(25, 25, 30, 0.95)';
        content.style.borderTop = '1px solid var(--border)';

        content.appendChild(renderAttestationAttrs(attrs));
        element.appendChild(content);
    }

    // Morph wiring: canvas ↔ window ↔ tray
    const title = attestation
        ? `${attestation.subjects?.join(', ') || '?'} is ${attestation.predicates?.join(', ') || '?'}`
        : 'Attestation';

    wireExpandToWindow({
        element,
        expandBtn,
        glyphId: glyph.id,
        title,
        symbol: AS,
        renderContent: () => buildAttestationContent(attestation, attrs),
        logLabel: 'AsGlyph',
    });

    log.debug(SEG.GLYPH, `[AsGlyph] Created attestation glyph ${glyph.id} (attrs: ${hasContent})`);

    return element;
}

/**
 * Spawn an attestation glyph on the canvas from an attestation object.
 */
export function spawnAttestationGlyph(attestation: Attestation, mouseX?: number, mouseY?: number): void {
    const attrs = parseAttributes(attestation);
    spawnOnCanvas({
        symbol: AS,
        prefix: 'as',
        title: 'Attestation',
        content: JSON.stringify(attestation),
        fallbackWidth: 420,
        fallbackHeight: attrs ? 200 : 28,
        mouseX,
        mouseY,
    });
}

/**
 * Spawn an attestation directly as a window via glyphRun (tray→window path).
 * No canvas detour — the element starts as a tray dot and immediately morphs to window.
 * The window includes a "place on canvas" button for the window→canvas transition.
 */
export function spawnAttestationAsWindow(attestation: Attestation): void {
    const glyphId = `as-${attestation.id || crypto.randomUUID()}`;

    // Dedup: check if this attestation already exists in any state
    const existing = document.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
    if (existing) {
        if (isInWindowState(existing)) {
            // Already a window — bring to front
            existing.style.zIndex = '1001';
            setTimeout(() => { existing.style.zIndex = '1000'; }, 2000);
        } else {
            // On canvas — fade the panel, pulse the glyph
            revealGlyphOnCanvas(existing);
        }
        log.debug(SEG.GLYPH, `[AsGlyph] Attestation ${glyphId} already exists, highlighting`);
        return;
    }
    if (glyphRun.has(glyphId)) {
        // In tray (minimized) — open as window
        glyphRun.openGlyph(glyphId);
        return;
    }

    const attrs = parseAttributes(attestation);
    const subjects = attestation.subjects?.join(', ') || '?';
    const predicates = attestation.predicates?.join(', ') || '?';
    const title = `${subjects} is ${predicates}`;

    glyphRun.add({
        id: glyphId,
        title,
        symbol: AS,
        initialWidth: '420px',
        initialHeight: attrs ? '300px' : '200px',
        onClose: () => {
            glyphRun.remove(glyphId);
            log.debug(SEG.GLYPH, `[AsGlyph] Closed window ${glyphId}`);
        },
        renderTitleBar: () => buildAttestationTitleBar(attestation, glyphId),
        renderContent: () => buildAttestationContent(attestation, attrs),
    });

    glyphRun.openGlyph(glyphId);
    log.debug(SEG.GLYPH, `[AsGlyph] Spawned attestation ${glyphId} as window`);
}

/**
 * Build the attestation title bar for the window manifestation.
 * Includes: AS symbol, triple text, place-on-canvas button, metadata pill.
 */
function buildAttestationTitleBar(attestation: Attestation, glyphId: string): HTMLElement {
    const titleBar = document.createElement('div');
    titleBar.className = 'glyph-title-bar glyph-title-bar--auto';
    titleBar.style.position = 'relative';

    const symbol = document.createElement('span');
    symbol.textContent = AS;
    symbol.style.fontWeight = 'bold';
    symbol.style.flexShrink = '0';
    symbol.style.color = AZURE;
    titleBar.appendChild(symbol);

    const tripleText = renderTriple(attestation, {
        palette: { value: AZURE_VALUE, keyword: AZURE_KEYWORD },
        showWatcherEyes: true,
    });
    titleBar.appendChild(tripleText);

    // Place-on-canvas button
    const placeBtn = document.createElement('button');
    placeBtn.className = 'titlebar-btn';
    placeBtn.textContent = '\u2B07'; // ⬇
    placeBtn.title = 'Place on canvas';
    placeBtn.style.flexShrink = '0';
    placeBtn.style.marginLeft = 'auto';
    preventDrag(placeBtn);
    titleBar.appendChild(placeBtn);

    placeBtn.addEventListener('click', (e) => {
        // Stop propagation — glyphRun has a click handler on the element that would
        // re-trigger morphToWindow if the click bubbles up
        e.stopPropagation();
        const element = placeBtn.closest('[data-glyph-id]') as HTMLElement | null;
        if (!element) return;
        placeAttestationWindowOnCanvas(element, attestation, glyphId, placeBtn);
    });

    // Metadata pill
    const metaLines = buildMetaLines(attestation);
    if (metaLines.length > 0) {
        const pill = document.createElement('div');
        pill.className = 'as-meta-pill';

        const metaPopover = document.createElement('div');
        metaPopover.className = 'meta-popover as-meta-popover';
        metaPopover.innerHTML = metaLines.join('<br>');

        pill.appendChild(metaPopover);
        titleBar.appendChild(pill);
    }

    return titleBar;
}

/**
 * Place an attestation window onto the canvas.
 * Transitions from tray-originated window to canvas-placed element.
 */
function placeAttestationWindowOnCanvas(
    element: HTMLElement,
    attestation: Attestation,
    glyphId: string,
    placeBtn: HTMLElement,
): void {
    if (!isInWindowState(element)) return;

    const canvasEl = document.querySelector('.canvas-workspace') as HTMLElement | null;
    if (!canvasEl) {
        log.warn(SEG.GLYPH, `[AsGlyph] No canvas workspace found, cannot place ${glyphId}`);
        return;
    }
    const canvasId = canvasEl.dataset.canvasId ?? 'canvas-workspace';
    const contentLayer = canvasEl.querySelector('.canvas-content-layer') as HTMLElement | null;
    if (!contentLayer) {
        log.warn(SEG.GLYPH, `[AsGlyph] No content layer in canvas ${canvasId}`);
        return;
    }

    // Capture window position before teardown
    const windowRect = element.getBoundingClientRect();
    const canvasRect = canvasEl.getBoundingClientRect();

    // Convert window screen position to canvas-local coordinates
    const relX = windowRect.left - canvasRect.left;
    const relY = windowRect.top - canvasRect.top;
    const canvasPos = screenToCanvas(canvasId, relX, relY);

    // Tear down window state
    teardownWindowDrag(element);
    const resizeObserver = (element as any).__resizeObserver as ResizeObserver | undefined;
    if (resizeObserver) {
        resizeObserver.disconnect();
        delete (element as any).__resizeObserver;
    }
    const titleBar = element.querySelector('.glyph-title-bar') as HTMLElement | null;
    if (titleBar) removeWindowControls(titleBar);

    // Unwrap .canvas-window-content if morphToWindow wrapped children
    const contentDiv = element.querySelector('.canvas-window-content');
    if (contentDiv) {
        while (contentDiv.firstChild) {
            element.appendChild(contentDiv.firstChild);
        }
        contentDiv.remove();
    }

    // Clear window state
    setWindowState(element, false);

    // Remove from body, clear all inline styles
    element.remove();
    element.style.cssText = '';

    // Untrack from glyphRun — element is leaving tray management for canvas.
    // Called while detached so glyphRun.remove()'s element.remove() is a no-op.
    // If the user later minimizes to tray, glyphRun.adopt() will re-add it.
    if (glyphRun.has(glyphId)) {
        glyphRun.remove(glyphId);
    }

    // Set canvas-placed positioning
    const width = 420;
    const attrs = parseAttributes(attestation);
    const height = attrs ? 200 : 28;
    element.style.position = 'absolute';
    element.style.left = `${Math.round(canvasPos.x)}px`;
    element.style.top = `${Math.round(canvasPos.y)}px`;
    element.style.width = `${width}px`;
    element.style.height = `${height}px`;
    element.style.minWidth = '200px';
    element.classList.add('canvas-glyph', 'canvas-attestation-glyph');

    // Reparent to canvas
    contentLayer.appendChild(element);

    // Build glyph object for drag/resize handlers
    const title = `${attestation.subjects?.join(', ') || '?'} is ${attestation.predicates?.join(', ') || '?'}`;
    const glyph: Glyph = {
        id: glyphId,
        title,
        symbol: AS,
        x: Math.round(canvasPos.x),
        y: Math.round(canvasPos.y),
        content: JSON.stringify(attestation),
        renderContent: () => buildAttestationContent(attestation, attrs),
    };

    // Add drag/resize handlers
    if (titleBar) {
        const cleanupDrag = makeDraggable(element, titleBar, glyph, { logLabel: 'AsGlyph' });
        storeCleanup(element, cleanupDrag);
    }
    if (attrs) {
        const resizeHandle = document.createElement('div');
        resizeHandle.className = 'glyph-resize-handle';
        element.appendChild(resizeHandle);
        const cleanupResize = makeResizable(element, resizeHandle, glyph, { logLabel: 'AsGlyph' });
        storeCleanup(element, cleanupResize);
    }

    // Track in uiState
    uiState.addCanvasGlyph({
        id: glyphId,
        symbol: AS,
        x: Math.round(canvasPos.x),
        y: Math.round(canvasPos.y),
        width,
        height,
        content: JSON.stringify(attestation),
    });

    // Swap button to expand (canvas→window direction)
    placeBtn.textContent = '\u2B06'; // ⬆
    placeBtn.title = 'Expand to window';

    // Re-wire button for canvas→window morph
    const newBtn = placeBtn.cloneNode(true) as HTMLElement;
    placeBtn.replaceWith(newBtn);
    preventDrag(newBtn);

    wireExpandToWindow({
        element,
        expandBtn: newBtn,
        glyphId,
        title,
        symbol: AS,
        renderContent: () => buildAttestationContent(attestation, attrs),
        logLabel: 'AsGlyph',
        stopPropagation: true,
    });

    log.debug(SEG.GLYPH, `[AsGlyph] Placed ${glyphId} on canvas at (${Math.round(canvasPos.x)}, ${Math.round(canvasPos.y)})`);
}

/**
 * Reveal a glyph on canvas by fading the panel and pulsing the glyph border.
 * Panel fades to near-transparent for 2.5s, glyph pulses for 1.2s.
 */
function revealGlyphOnCanvas(glyphElement: HTMLElement): void {
    // Fade any open panel to reveal the canvas behind it
    const panel = document.querySelector('[data-glyph-id="embeddings-glyph"]') as HTMLElement | null;
    if (panel) {
        panel.style.transition = 'opacity 200ms ease-out';
        panel.style.opacity = '0.1';
        setTimeout(() => {
            panel.style.transition = 'opacity 400ms ease-in';
            panel.style.opacity = '1';
        }, 2500);
    }

    // Pulse the glyph border after a short delay (let the panel fade first)
    setTimeout(() => {
        glyphElement.classList.add('glyph-pulse');
        glyphElement.addEventListener('animationend', () => {
            glyphElement.classList.remove('glyph-pulse');
        }, { once: true });
    }, 250);
}

/**
 * Build attestation content for tray restoration.
 */
function buildAttestationContent(
    attestation: Attestation | null,
    attrs: Record<string, unknown> | null,
): HTMLElement {
    const outer = document.createElement('div');
    const wrapper = document.createElement('div');
    wrapper.className = 'glyph-content';
    outer.appendChild(wrapper);

    if (attestation) {
        // Triple
        const triple = document.createElement('div');
        triple.style.padding = '8px';
        triple.style.fontSize = '12px';
        triple.style.fontFamily = 'monospace';
        triple.style.color = AZURE_VALUE;
        triple.style.wordBreak = 'break-word';
        const s = attestation.subjects?.join(', ') || 'N/A';
        const p = attestation.predicates?.join(', ') || 'N/A';
        const c = attestation.contexts?.join(', ') || 'N/A';
        triple.textContent = `${s} is ${p} of ${c}`;
        wrapper.appendChild(triple);

        // Metadata
        const metaLines = buildMetaLines(attestation);
        if (metaLines.length > 0) {
            const meta = document.createElement('div');
            meta.style.padding = '4px 8px';
            meta.style.fontSize = '11px';
            meta.style.color = 'var(--text-secondary)';
            meta.innerHTML = metaLines.join('<br>');
            wrapper.appendChild(meta);
        }

        // Attributes
        if (attrs) {
            const attrDiv = renderAttestationAttrs(attrs);
            attrDiv.style.padding = '4px 8px';
            attrDiv.style.borderTop = '1px solid var(--border)';
            wrapper.appendChild(attrDiv);
        }
    }

    return outer;
}

function formatTimestamp(value: unknown): string {
    if (!value) return 'N/A';
    try {
        if (typeof value === 'string') {
            return new Date(value).toLocaleString();
        }
        if (typeof value === 'number') {
            // Unix seconds or milliseconds — if < 1e12, assume seconds
            const ms = value < 1e12 ? value * 1000 : value;
            return new Date(ms).toLocaleString();
        }
        return String(value);
    } catch {
        return String(value);
    }
}
