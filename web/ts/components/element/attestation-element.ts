/**
 * Attestation Element (+) — view a single attestation on canvas
 *
 * Opened via double-click on attestation result items in AX or SE elements.
 * Title bar IS the triple (subjects is predicates of contexts).
 * Attributes shown below title bar only when present.
 * No attributes → compact title-bar-only element.
 * Metadata (actors, source, timestamps, id) hidden by default —
 * revealed via hover pill at bottom center of title bar.
 */

import type { Element } from '@teranos/elements';
import { wireExpandToWindow, teardownWindowDrag, removeWindowControls, getForm, setForm, tray, createSymbolSpan, settleSymbolSpan } from '@teranos/elements';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { Attestation as AttestationSym } from '../../sym';
import { renderTriple } from './attestation-triple';
import { stripHtml } from '../../html-utils';
import { log, SEG } from '../../logger';
import { canvasPlaced } from '@teranos/elements';
import { preventDrag, makeDraggable, makeResizable, storeCleanup } from '@teranos/elements';
import { screenToCanvas } from './canvas/canvas-pan';
import { uiState } from '../../state/ui';
import { spawnOnCanvasDragging } from './spawn-on-canvas';
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
 * Create an Attestation element.
 * Title bar = triple. Attributes below if present. Metadata behind hover pill.
 */
export function createAttestationElement(item: Element): HTMLElement {
    let attestation: Attestation | null = null;
    try {
        if (item.content) {
            attestation = JSON.parse(item.content);
        }
    } catch (err) {
        log.warn(SEG.ELEMENT, `[AsElement] Failed to parse attestation content for ${item.id}:`, err);
    }

    const attrs = attestation ? parseAttributes(attestation) : null;

    // Title bar: + symbol + triple + expand button + metadata pill
    const titleBar = document.createElement('div');
    titleBar.className = 'title-bar title-bar--auto';
    titleBar.style.position = 'relative';

    // Settle a carried cursor span or render the symbol through the package —
    // element continuity across cursor → placed included
    const symbol = item.symbolElement ? settleSymbolSpan(item.symbolElement) : createSymbolSpan(AttestationSym);
    symbol.style.fontWeight = 'bold';
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
        item: item,
        className: 'canvas-attestation-element',
        defaults: { x: 200, y: 200, width: 420, height: hasContent ? 200 : 28 },
        resizable: hasContent,
        useMinHeight: true,
        logLabel: 'AsElement',
    });
    element.style.minWidth = '200px';

    element.appendChild(titleBar);

    // Attributes content — only when there are attributes to show
    if (attestation && attrs) {
        const content = document.createElement('div');
        content.className = 'content-area';
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
        elementId: item.id,
        title,
        symbol: AttestationSym,
        renderContent: () => buildAttestationContent(attestation, attrs),
        logLabel: 'AsElement',
    });

    log.debug(SEG.ELEMENT, `[AsElement] Created attestation element ${item.id} (attrs: ${hasContent})`);

    return element;
}

/**
 * Spawn an attestation element on the canvas from an attestation object.
 */
export function spawnAttestationElement(attestation: Attestation, mouseX?: number, mouseY?: number): void {
    const attrs = parseAttributes(attestation);
    spawnOnCanvasDragging({
        symbol: AttestationSym,
        prefix: 'as',
        title: 'Attestation',
        content: JSON.stringify(attestation),
        fallbackWidth: 420,
        fallbackHeight: attrs ? 200 : 28,
    }, mouseX || window.innerWidth / 2, mouseY || window.innerHeight / 2);
}

/**
 * Spawn an attestation directly as a window via tray (tray→window path).
 * No canvas detour — the element starts as a tray dot and immediately morphs to window.
 * The window includes a "place on canvas" button for the window→canvas transition.
 */
export function spawnAttestationAsWindow(attestation: Attestation): void {
    const itemId = `as-${attestation.id || crypto.randomUUID()}`;

    // Dedup: check if this attestation already exists in any state
    const existing = document.querySelector(`[data-element-id="${itemId}"]`) as HTMLElement | null;
    if (existing) {
        const manifestation = getForm(existing);
        if (manifestation === 'window' || manifestation === 'canvasExpanded') {
            // Already off the canvas — bring to front
            existing.style.zIndex = '1001';
            setTimeout(() => { existing.style.zIndex = '1000'; }, 2000);
        } else {
            // On canvas — fade the panel, pulse the element
            revealElementOnCanvas(existing);
        }
        log.debug(SEG.ELEMENT, `[AsElement] Attestation ${itemId} already exists, highlighting`);
        return;
    }
    if (tray.has(itemId)) {
        // In tray (minimized) — open as window
        tray.open(itemId);
        return;
    }

    const attrs = parseAttributes(attestation);
    const subjects = attestation.subjects?.join(', ') || '?';
    const predicates = attestation.predicates?.join(', ') || '?';
    // Element.title is plain text — this is the one title built from stored
    // content, so strip at the boundary (the package no longer strips)
    const title = stripHtml(`${subjects} is ${predicates}`);

    tray.add({
        id: itemId,
        title,
        symbol: AttestationSym,
        initialWidth: '420px',
        initialHeight: attrs ? '300px' : '200px',
        onClose: () => {
            tray.remove(itemId);
            log.debug(SEG.ELEMENT, `[AsElement] Closed window ${itemId}`);
        },
        renderTitleBar: () => buildAttestationTitleBar(attestation, itemId),
        renderContent: () => buildAttestationContent(attestation, attrs),
    });

    tray.open(itemId);
    log.debug(SEG.ELEMENT, `[AsElement] Spawned attestation ${itemId} as window`);
}

/**
 * Build the attestation title bar for the window manifestation.
 * Includes: ⎔ symbol, triple text, place-on-canvas button, metadata pill.
 */
function buildAttestationTitleBar(attestation: Attestation, itemId: string): HTMLElement {
    const titleBar = document.createElement('div');
    titleBar.className = 'title-bar title-bar--auto';
    titleBar.style.position = 'relative';

    const symbol = createSymbolSpan(AttestationSym);
    symbol.style.fontWeight = 'bold';
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
        // Stop propagation — tray has a click handler on the element that would
        // re-trigger morphCanvasPlacedToWindow if the click bubbles up
        e.stopPropagation();
        const element = placeBtn.closest('[data-element-id]') as HTMLElement | null;
        if (!element) return;
        placeAttestationWindowOnCanvas(element, attestation, itemId, placeBtn);
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
    itemId: string,
    placeBtn: HTMLElement,
): void {
    const manifestation = getForm(element);
    if (manifestation !== 'window' && manifestation !== 'canvasExpanded') return;

    const canvasEl = document.querySelector('.canvas-workspace') as HTMLElement | null;
    if (!canvasEl) {
        log.warn(SEG.ELEMENT, `[AsElement] No canvas workspace found, cannot place ${itemId}`);
        return;
    }
    const canvasId = canvasEl.dataset.canvasId ?? 'canvas-workspace';
    const contentLayer = canvasEl.querySelector('.canvas-content-layer') as HTMLElement | null;
    if (!contentLayer) {
        log.warn(SEG.ELEMENT, `[AsElement] No content layer in canvas ${canvasId}`);
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
    const titleBar = element.querySelector('.title-bar') as HTMLElement | null;
    if (titleBar) removeWindowControls(titleBar);

    // Unwrap .canvas-window-content if morphCanvasPlacedToWindow wrapped children
    const contentDiv = element.querySelector('.canvas-window-content');
    if (contentDiv) {
        while (contentDiv.firstChild) {
            element.appendChild(contentDiv.firstChild);
        }
        contentDiv.remove();
    }

    // On the canvas now, and the element says so
    setForm(element, 'canvasPlaced');

    // Remove from body, clear all inline styles
    element.remove();
    element.style.cssText = '';

    // Untrack from tray — element is leaving tray management for canvas.
    // Called while detached so tray.remove()'s element.remove() is a no-op.
    // If the user later minimizes to tray, tray.adopt() will re-add it.
    if (tray.has(itemId)) {
        tray.remove(itemId);
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
    element.classList.add('canvas-element', 'canvas-attestation-element');

    // Reparent to canvas
    contentLayer.appendChild(element);

    // Build element object for drag/resize handlers
    const title = `${attestation.subjects?.join(', ') || '?'} is ${attestation.predicates?.join(', ') || '?'}`;
    const item: Element = {
        id: itemId,
        title,
        symbol: AttestationSym,
        x: Math.round(canvasPos.x),
        y: Math.round(canvasPos.y),
        content: JSON.stringify(attestation),
        renderContent: () => buildAttestationContent(attestation, attrs),
    };

    // Add drag/resize handlers
    if (titleBar) {
        const cleanupDrag = makeDraggable(element, titleBar, item, { logLabel: 'AsElement' });
        storeCleanup(element, cleanupDrag);
    }
    if (attrs) {
        const resizeHandle = document.createElement('div');
        resizeHandle.className = 'resize-handle';
        element.appendChild(resizeHandle);
        const cleanupResize = makeResizable(element, resizeHandle, item, { logLabel: 'AsElement' });
        storeCleanup(element, cleanupResize);
    }

    // Track in uiState
    uiState.addCanvasElement({
        id: itemId,
        symbol: AttestationSym,
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
        elementId: itemId,
        title,
        symbol: AttestationSym,
        renderContent: () => buildAttestationContent(attestation, attrs),
        logLabel: 'AsElement',
        stopPropagation: true,
    });

    log.debug(SEG.ELEMENT, `[AsElement] Placed ${itemId} on canvas at (${Math.round(canvasPos.x)}, ${Math.round(canvasPos.y)})`);
}

/**
 * Reveal an element on canvas by fading the panel and pulsing the element border.
 * Panel fades to near-transparent for 2.5s, element pulses for 1.2s.
 */
function revealElementOnCanvas(elementElement: HTMLElement): void {
    // Fade any open panel to reveal the canvas behind it
    const panel = document.querySelector('[data-element-id="embeddings-element"]') as HTMLElement | null;
    if (panel) {
        panel.style.transition = 'opacity 200ms ease-out';
        panel.style.opacity = '0.1';
        setTimeout(() => {
            panel.style.transition = 'opacity 400ms ease-in';
            panel.style.opacity = '1';
        }, 2500);
    }

    // Pulse the element border after a short delay (let the panel fade first)
    setTimeout(() => {
        elementElement.classList.add('element-pulse');
        elementElement.addEventListener('animationend', () => {
            elementElement.classList.remove('element-pulse');
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
    wrapper.className = 'element-content';
    outer.appendChild(wrapper);

    if (attestation) {
        // Triple
        const triple = document.createElement('div');
        triple.style.padding = '8px';
        triple.style.fontSize = '12px';
        triple.style.fontFamily = 'var(--font-mono)';
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
    } catch (notADate) {
        return String(value);
    }
}
