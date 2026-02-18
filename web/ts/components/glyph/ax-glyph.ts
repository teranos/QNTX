/**
 * Ax Glyph - Ax query editor on canvas grid
 *
 * Text editor for writing ax queries like "is git", "has certification", etc.
 * Similar to ATS editor or prompt editor - just the query text itself.
 *
 * ARCHITECTURE:
 * Canvas ax glyphs are lightweight references/thumbnails that show the query text.
 * They serve as visual markers on the spatial canvas to show active ax queries.
 *
 * Uses canvasPlaced() for shared infrastructure (positioning, drag, resize, cleanup,
 * CSS state) with a custom title bar (same pattern as IX glyph).
 *
 * TODO: Future enhancements
 * - Add result count badge (e.g., "42 attestations")
 * - Show mini type distribution (tiny bar chart or colored dots for node types)
 * - Click handler to spawn full ax manifestation (attestation explorer window)
 * - Integration with graph explorer (may reuse existing graph component)
 * - Persist query results to avoid re-execution on reload
 * - Support query templates/snippets
 */

import type { Glyph } from './glyph';
import { AX } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { preventDrag, storeCleanup, cleanupResizeObserver } from './glyph-interaction';
import { canvasPlaced } from './manifestations/canvas-placed';
import { sendMessage } from '../../websocket';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { queryAttestations, parseQuery } from '../../qntx-wasm';
import { tooltip } from '../tooltip';
import { spawnAttestationGlyph } from './attestation-glyph';
import { uiState } from '../../state/ui';
import { syncStateManager } from '../../state/sync-state';
import { connectivityManager } from '../../connectivity';
import {
    CANVAS_GLYPH_TITLE_BAR_HEIGHT,
    MAX_VIEWPORT_HEIGHT_RATIO
} from './glyph';

function appendEmptyState(container: HTMLElement): void {
    const empty = document.createElement('div');
    empty.className = 'ax-glyph-empty-state';
    empty.textContent = 'No matches yet';
    empty.style.color = 'var(--text-secondary)';
    empty.style.textAlign = 'center';
    empty.style.padding = '20px';
    container.appendChild(empty);
}

/**
 * Create an AX glyph using canvasPlaced() with custom title bar (IX pattern).
 *
 * @param glyph - Glyph model with id, position, and size
 * @returns The canvas-placed HTMLElement
 */
export function createAxGlyph(glyph: Glyph): HTMLElement {
    const glyphId = glyph.id;

    // Load persisted query from canvas state (or glyph argument on restore)
    const existingGlyph = uiState.getCanvasGlyphs().find(g => g.id === glyphId);
    let currentQuery = existingGlyph?.content ?? glyph.content ?? '';

    // Symbol (draggable area) — created before canvasPlaced to use as drag handle
    const symbol = document.createElement('span');
    symbol.textContent = AX;
    symbol.style.cursor = 'move';
    symbol.style.fontWeight = 'bold';
    symbol.style.flexShrink = '0';
    symbol.style.color = 'var(--glyph-status-running-text)';

    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-ax-glyph',
        defaults: { x: 200, y: 200, width: 400, height: 200 },
        dragHandle: symbol,
        resizable: true,
        logLabel: 'AxGlyph',
    });
    element.style.minWidth = '200px';
    element.style.minHeight = '120px';

    // Single-line query input (takes remaining space)
    const editor = document.createElement('input');
    editor.type = 'text';
    editor.className = 'ax-query-input';
    editor.value = currentQuery;
    editor.placeholder = 'Enter ax query (e.g., ALICE, is git)';
    editor.style.flex = '1';
    editor.style.padding = '4px 8px';
    editor.style.fontSize = '13px';
    editor.style.fontFamily = 'monospace';
    editor.style.border = 'none';
    editor.style.outline = 'none';
    editor.style.backgroundColor = 'rgba(25, 25, 30, 0.95)';
    editor.style.color = '#d4f0d4'; // 20% greener and whiter
    editor.style.borderRadius = '2px';

    preventDrag(editor);

    // Title bar (custom layout: symbol + query input) — shared CSS class for state styling
    const titleBar = document.createElement('div');
    titleBar.className = 'canvas-glyph-title-bar';
    titleBar.style.padding = '4px 4px 4px 8px'; // Compact: reduced top/bottom/right, keep left for symbol

    titleBar.appendChild(symbol);
    titleBar.appendChild(editor);

    element.appendChild(titleBar);

    // Title bar background must track container state (opaque bg blocks parent tint)
    const COLOR_STATES = {
        idle:    { container: 'rgba(30, 30, 35, 0.92)',  titleBar: 'var(--bg-tertiary)' },
        pending: { container: 'rgba(42, 43, 61, 0.92)',  titleBar: 'rgba(42, 43, 61, 0.92)' },
        orange:  { container: 'rgba(61, 45, 20, 0.92)',  titleBar: '#5c3d1a' },
        teal:    { container: 'rgba(31, 61, 61, 0.92)',  titleBar: '#1f3d3d' },
    } as const;

    function setColorState(state: keyof typeof COLOR_STATES) {
        element.style.backgroundColor = COLOR_STATES[state].container;
        titleBar.style.backgroundColor = COLOR_STATES[state].titleBar;
    }

    setColorState('idle');

    // Results container - scrollable list of matched attestations (gets all remaining space)
    const resultsContainer = document.createElement('div');
    resultsContainer.className = 'ax-glyph-results';
    resultsContainer.style.flex = '1';
    resultsContainer.style.overflow = 'auto';
    resultsContainer.style.padding = '8px';
    resultsContainer.style.backgroundColor = 'rgba(25, 25, 30, 0.95)';
    resultsContainer.style.borderTop = '1px solid var(--border-color)';
    resultsContainer.style.fontSize = '12px';
    resultsContainer.style.fontFamily = 'monospace';

    appendEmptyState(resultsContainer);

    element.appendChild(resultsContainer);

    // Attach tooltip support for attestation results
    tooltip.attach(resultsContainer, '.ax-glyph-result-item');

    // Shared: run local IndexedDB query, populate results, update color state
    async function runLocalQuery(): Promise<void> {
        const query = currentQuery.trim();
        if (!query) return;

        // Clear and re-populate results from IndexedDB
        resultsContainer.innerHTML = '';
        try {
            const parsed = parseQuery(query);
            if (parsed.ok) {
                const localResults = await queryAttestations(parsed.query);
                const displayedIds = new Set<string>();
                for (const att of localResults) {
                    if (att.id) displayedIds.add(att.id);
                    resultsContainer.appendChild(renderAttestation(att));
                }
                (element as any)._localIds = displayedIds;

                if (localResults.length === 0) {
                    appendEmptyState(resultsContainer);
                }

                log.debug(SEG.GLYPH, `[AxGlyph] Local query: ${localResults.length} results for ${glyphId}`);
            }
        } catch (err) {
            log.debug(SEG.GLYPH, `[AxGlyph] Local query failed for ${glyphId}:`, err);
            appendEmptyState(resultsContainer);
        }

        // Update color + data attributes
        element.dataset.localActive = 'true';
        resultsContainer.dataset.localActive = 'true';

        if (connectivityManager.state === 'online') {
            sendMessage({
                type: 'watcher_upsert',
                watcher_id: `ax-glyph-${glyphId}`,
                watcher_query: query,
                watcher_name: `AX Glyph: ${query.substring(0, 30)}${query.length > 30 ? '...' : ''}`,
                enabled: true
            });
            setColorState('teal');
        } else {
            setColorState('orange');
        }
    }

    // Auto-save and watcher update with debouncing (500ms delay)
    let saveTimeout: number | undefined;
    editor.addEventListener('input', () => {
        currentQuery = editor.value;

        // Clear existing timeout
        if (saveTimeout !== undefined) {
            clearTimeout(saveTimeout);
        }

        // Update background to indicate pending state
        setColorState('pending');

        // Clear results immediately when query changes
        resultsContainer.innerHTML = '';
        appendEmptyState(resultsContainer);

        // Debounce save and watcher update for 500ms
        saveTimeout = window.setTimeout(async () => {
            const existing = uiState.getCanvasGlyphs().find(g => g.id === glyphId);
            if (existing) {
                uiState.addCanvasGlyph({ ...existing, content: currentQuery });
            }

            if (!currentQuery.trim()) {
                setColorState('idle');
                return;
            }

            await runLocalQuery();
            log.debug(SEG.GLYPH, `[AxGlyph] Query updated for ${glyphId}: "${currentQuery}"`);
        }, 500);
    });

    // Set up ResizeObserver for auto-sizing glyph to content
    setupAxGlyphResizeObserver(element, resultsContainer, glyphId);

    // Subscribe to sync state changes for visual feedback
    const syncUnsub = syncStateManager.subscribe(glyphId, (state) => {
        element.dataset.syncState = state;
    });
    storeCleanup(element, syncUnsub);

    // Subscribe to connectivity state changes — re-fire local query on transition
    const connectUnsub = connectivityManager.subscribe((state) => {
        element.dataset.connectivityMode = state;
        resultsContainer.dataset.connectivityMode = state;

        // Re-query IndexedDB on connectivity change (picks up new local attestations + updates color)
        if (currentQuery.trim()) {
            void runLocalQuery();
        }
    });
    storeCleanup(element, connectUnsub);

    return element;
}

/**
 * Set up ResizeObserver to auto-size AX glyph to match results content height
 * Works alongside manual resize handles - user can still drag to resize
 */
function setupAxGlyphResizeObserver(
    glyphElement: HTMLElement,
    resultsContainer: HTMLElement,
    glyphId: string
): void {
    // Cleanup any existing observer to prevent memory leaks on re-render
    cleanupResizeObserver(glyphElement, `AX ${glyphId}`);

    const titleBarHeight = CANVAS_GLYPH_TITLE_BAR_HEIGHT;
    const maxHeight = window.innerHeight * MAX_VIEWPORT_HEIGHT_RATIO;

    const resizeObserver = new ResizeObserver(entries => {
        for (const entry of entries) {
            const contentHeight = entry.contentRect.height;
            const totalHeight = Math.min(contentHeight + titleBarHeight, maxHeight);

            // Update minHeight instead of height to allow manual resize
            glyphElement.style.minHeight = `${totalHeight}px`;

            log.debug(SEG.GLYPH, `[AX ${glyphId}] Auto-resized to ${totalHeight}px (content: ${contentHeight}px)`);
        }
    });

    resizeObserver.observe(resultsContainer);

    // Store observer for cleanup
    (glyphElement as any).__resizeObserver = resizeObserver;
}

/**
 * Render a single attestation result in the results list
 */
function renderAttestation(attestation: Attestation): HTMLElement {
    const item = document.createElement('div');
    item.className = 'ax-glyph-result-item has-tooltip';
    item.style.padding = '8px';
    item.style.marginBottom = '4px';
    item.style.backgroundColor = 'rgba(31, 61, 31, 0.35)'; // 20% greener tint
    item.style.borderRadius = '2px';
    item.style.cursor = 'pointer';

    // Store full attestation for double-click spawn
    item.dataset.attestation = JSON.stringify(attestation);
    item.addEventListener('dblclick', (e) => {
        e.stopPropagation();
        spawnAttestationGlyph(attestation, e.clientX, e.clientY);
    });

    // Format attestation data as natural language: "SUBJECTS is PREDICATES of CONTEXTS"
    const subjects = attestation.subjects?.join(', ') || 'N/A';
    const predicates = attestation.predicates?.join(', ') || 'N/A';
    const contexts = attestation.contexts?.join(', ') || 'N/A';

    // Create single-line natural language format with darker keywords
    const text = document.createElement('div');
    text.style.fontSize = '11px';
    text.style.color = '#d4f0d4'; // 20% greener and whiter
    text.style.fontFamily = 'monospace';
    text.style.whiteSpace = 'nowrap';
    text.style.overflow = 'hidden';
    text.style.textOverflow = 'ellipsis';

    // Build formatted text with darker keywords (textContent per span to prevent XSS)
    const subjectSpan = document.createElement('span');
    subjectSpan.style.color = '#d4f0d4';
    subjectSpan.textContent = subjects;

    const isSpan = document.createElement('span');
    isSpan.style.color = '#6b7b6b';
    isSpan.textContent = ' is ';

    const predSpan = document.createElement('span');
    predSpan.style.color = '#d4f0d4';
    predSpan.textContent = predicates;

    const ofSpan = document.createElement('span');
    ofSpan.style.color = '#6b7b6b';
    ofSpan.textContent = ' of ';

    const ctxSpan = document.createElement('span');
    ctxSpan.style.color = '#d4f0d4';
    ctxSpan.textContent = contexts;

    text.append(subjectSpan, isSpan, predSpan, ofSpan, ctxSpan);

    // Add attestation ID as tooltip using tooltip infrastructure
    item.dataset.tooltip = attestation.id || 'unknown';
    item.appendChild(text);

    return item;
}

/**
 * Update the results display with new attestations
 */
export function updateAxGlyphResults(glyphId: string, attestation: Attestation): void {
    const glyph = document.querySelector(`[data-glyph-id="${glyphId}"]`);
    if (!glyph) {
        log.warn(SEG.GLYPH, `[AxGlyph] Cannot update results: glyph ${glyphId} not found in DOM`);
        return;
    }

    const resultsContainer = glyph.querySelector('.ax-glyph-results') as HTMLElement;
    if (!resultsContainer) {
        log.warn(SEG.GLYPH, `[AxGlyph] Cannot update results: results container not found for ${glyphId}`);
        return;
    }

    // Remove empty state if present
    const emptyState = resultsContainer.querySelector('.ax-glyph-empty-state');
    if (emptyState) {
        emptyState.remove();
    }

    // Remove error display if present (successful match clears error)
    const errorDisplay = resultsContainer.querySelector('.ax-glyph-error');
    if (errorDisplay) {
        errorDisplay.remove();
    }

    // Dedup: skip if already shown from local IndexedDB query
    if (attestation.id) {
        const localIds = (glyph as any)._localIds as Set<string> | undefined;
        if (localIds?.has(attestation.id)) {
            log.debug(SEG.GLYPH, `[AxGlyph] Skipped duplicate ${attestation.id} (already from local)`);
            return;
        }
    }

    // Add new result at top (most recent first)
    const resultItem = renderAttestation(attestation);
    resultsContainer.insertBefore(resultItem, resultsContainer.firstChild);

    log.debug(SEG.GLYPH, `[AxGlyph] Added result to ${glyphId}:`, attestation.id);
}

/**
 * Update AX glyph with error message and optional structured details
 * Called by WebSocket handler when watcher_error message arrives
 */
export function updateAxGlyphError(glyphId: string, errorMsg: string, severity: string, details?: string[]): void {
    // Find the glyph element by data attribute
    const glyph = document.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement;
    if (!glyph) {
        log.warn(SEG.GLYPH, `[AxGlyph] Cannot update error: glyph ${glyphId} not found in DOM`);
        return;
    }

    const resultsContainer = glyph.querySelector('.ax-glyph-results') as HTMLElement;
    if (!resultsContainer) {
        log.warn(SEG.GLYPH, `[AxGlyph] Cannot update error: results container not found for ${glyphId}`);
        return;
    }

    // Remove empty state if present
    const emptyState = resultsContainer.querySelector('.ax-glyph-empty-state');
    if (emptyState) {
        emptyState.remove();
    }

    // Remove existing error display if present
    const existingError = resultsContainer.querySelector('.ax-glyph-error');
    if (existingError) {
        existingError.remove();
    }

    // Create compact error display (like prompt glyph)
    const errorDisplay = document.createElement('div');
    errorDisplay.className = 'ax-glyph-error';
    errorDisplay.style.padding = '6px 8px';
    errorDisplay.style.fontSize = '11px'; // Smaller font
    errorDisplay.style.fontFamily = 'monospace';
    errorDisplay.style.backgroundColor = severity === 'error' ? 'var(--glyph-status-error-section-bg)' : '#2b2b1a';
    errorDisplay.style.color = severity === 'error' ? '#ff9999' : '#ffcc66';
    errorDisplay.style.whiteSpace = 'pre-wrap';
    errorDisplay.style.wordBreak = 'break-word';
    errorDisplay.style.overflowWrap = 'anywhere';
    errorDisplay.style.maxWidth = '100%';

    // Inline severity label + message (more compact)
    errorDisplay.textContent = `${severity.toUpperCase()}: ${errorMsg}`;

    // Add structured details if present (more compact)
    if (details && details.length > 0) {
        errorDisplay.textContent += '\n\n' + details.map(d => `  ${d}`).join('\n');
    }

    // Add error display at top of results
    resultsContainer.insertBefore(errorDisplay, resultsContainer.firstChild);

    // Update glyph + title bar background to indicate error state
    const errorBg = severity === 'error' ? 'rgba(61, 31, 31, 0.92)' : 'rgba(61, 61, 31, 0.92)';
    const errorTitleBg = severity === 'error' ? '#3d1f1f' : '#3d3d1f';
    glyph.style.backgroundColor = errorBg;
    const errorTitleBar = glyph.querySelector('.canvas-glyph-title-bar') as HTMLElement;
    if (errorTitleBar) errorTitleBar.style.backgroundColor = errorTitleBg;

    log.debug(SEG.GLYPH, `[AxGlyph] Displayed ${severity} for ${glyphId}:`, errorMsg);
}
