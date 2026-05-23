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
 * CSS state) with a custom title bar.
 *
 * TODO: Future enhancements
 * - TODO(#672): Attribute filters — expose watcher attribute_filters as UI conditions
 * - Add result count badge (e.g., "42 attestations")
 * - Show mini type distribution (tiny bar chart or colored dots for node types)
 * - Click handler to spawn full ax manifestation (attestation explorer window)
 * - Integration with graph explorer (may reuse existing graph component)
 * - Persist query results to avoid re-execution on reload
 * - Support query templates/snippets
 */

import type { Glyph } from '@qntx/glyphs';
import { AX } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { preventDrag, storeCleanup, setupGlyphResizeObserver } from '@qntx/glyphs';
import { canvasPlaced } from '@qntx/glyphs';
import { sendMessage, connectivity } from '../../client';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { queryAttestations, parseQuery } from '../../qntx-wasm';
import { tooltip } from '../tooltip';
import { spawnAttestationGlyph } from './attestation-glyph';
import { isSigmaAttestation, renderSigmaResultLine } from './sigma-glyph';
import { isTypeAttestation, groupTypeAttestations, renderTypeResultLine } from './type-result-line';
import { tripletKey, groupByTriplet, renderTripletResultLine } from './triplet-glyph';
import { renderTriple } from './attestation-triple';
import { uiState } from '../../state/ui';
import { syncStateManager } from '../../state/sync-state';
import {
    createColorStateSetter,
    appendEmptyState,
    showQueryError,
} from './query-glyph-states';

/**
 * Create an AX glyph using canvasPlaced() with custom title bar.
 *
 * @param glyph - Glyph model with id, position, and size
 * @returns The canvas-placed HTMLElement
 */
export function createAxGlyph(glyph: Glyph): HTMLElement {
    const glyphId = glyph.id;

    // Load persisted query from canvas state (or glyph argument on restore)
    const existingGlyph = uiState.getCanvasGlyph(glyphId);
    let currentQuery = existingGlyph?.content ?? glyph.content ?? '';

    // Symbol (draggable area) — reuse cursor symbol span if available
    const symbol = glyph.symbolElement ?? document.createElement('span');
    if (!glyph.symbolElement) symbol.textContent = AX;
    symbol.classList.remove('glyph-cursor-symbol');
    symbol.style.cursor = 'move';
    symbol.style.fontWeight = 'bold';
    symbol.style.flex = 'none';
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
    titleBar.className = 'glyph-title-bar';
    titleBar.style.padding = '4px 4px 4px 8px'; // Compact: reduced top/bottom/right, keep left for symbol

    titleBar.appendChild(symbol);
    titleBar.appendChild(editor);

    element.appendChild(titleBar);

    // Title bar background must track container state (opaque bg blocks parent tint)
    const setColorState = createColorStateSetter(element, titleBar);
    setColorState('idle');

    // Results container - scrollable list of matched attestations (gets all remaining space)
    const resultsContainer = document.createElement('div');
    resultsContainer.className = 'ax-glyph-results glyph-content-area';
    resultsContainer.style.backgroundColor = 'rgba(25, 25, 30, 0.95)';
    resultsContainer.style.borderTop = '1px solid var(--border)';
    resultsContainer.style.fontSize = '12px';
    resultsContainer.style.fontFamily = 'monospace';

    appendEmptyState(resultsContainer, 'ax-glyph-empty-state');

    element.appendChild(resultsContainer);

    // Attach tooltip support for attestation results
    tooltip.attach(resultsContainer, '.ax-glyph-result-item');

    // Shared: run local IndexedDB query, populate results, update color state
    async function runLocalQuery(): Promise<void> {
        const query = currentQuery.trim();
        if (!query) return;

        // Clear and show searching indicator
        resultsContainer.innerHTML = '';
        const searchingEl = document.createElement('div');
        searchingEl.style.padding = '8px';
        searchingEl.style.fontSize = '11px';
        searchingEl.style.color = 'var(--text-secondary)';
        searchingEl.style.fontFamily = 'monospace';
        searchingEl.textContent = 'searching...';
        resultsContainer.appendChild(searchingEl);

        try {
            const parsed = parseQuery(query);
            if (parsed.ok) {
                const localResults = await queryAttestations(parsed.query);
                searchingEl.remove();
                const displayedIds = new Set<string>();
                // Separate type attestations for subject grouping
                const typeAtts: Attestation[] = [];
                const otherAtts: Attestation[] = [];
                for (const att of localResults) {
                    if (att.id) displayedIds.add(att.id);
                    if (isTypeAttestation(att)) {
                        typeAtts.push(att);
                    } else {
                        otherAtts.push(att);
                    }
                }
                // Render grouped type attestations first
                for (const group of groupTypeAttestations(typeAtts)) {
                    resultsContainer.appendChild(renderTypeResultLine(group));
                }
                // Separate sigmas from regular attestations
                const sigmaAtts: Attestation[] = [];
                const regularAtts: Attestation[] = [];
                for (const att of otherAtts) {
                    if (isSigmaAttestation(att)) {
                        sigmaAtts.push(att);
                    } else {
                        regularAtts.push(att);
                    }
                }
                // Group regular attestations by triplet
                for (const [key, group] of groupByTriplet(regularAtts)) {
                    let row: HTMLElement;
                    if (group.length === 1) {
                        row = renderAttestation(group[0]);
                    } else {
                        row = renderTripletResultLine(group);
                    }
                    // Tag for streaming merge
                    row.dataset.tripletKey = key;
                    row.dataset.tripletAttestations = JSON.stringify(group);
                    resultsContainer.appendChild(row);
                }
                // Render sigmas after triplet groups
                for (const att of sigmaAtts) {
                    resultsContainer.appendChild(renderSigmaResultLine(att));
                }
                (element as any)._localIds = displayedIds;

                if (localResults.length === 0) {
                    appendEmptyState(resultsContainer, 'ax-glyph-empty-state');
                }

                log.debug(SEG.GLYPH, `[AxGlyph] Local query: ${localResults.length} results for ${glyphId}`);
            } else {
                searchingEl.remove();
            }
        } catch (err) {
            log.debug(SEG.GLYPH, `[AxGlyph] Local query failed for ${glyphId}:`, err);
            appendEmptyState(resultsContainer, 'ax-glyph-empty-state');
        }

        // Update color + data attributes
        element.dataset.localActive = 'true';
        resultsContainer.dataset.localActive = 'true';

        if (connectivity.state === 'online') {
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
        appendEmptyState(resultsContainer, 'ax-glyph-empty-state');

        // Debounce save and watcher update for 500ms
        saveTimeout = window.setTimeout(async () => {
            const existing = uiState.getCanvasGlyph(glyphId);
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
    setupGlyphResizeObserver(element, resultsContainer, `AX ${glyphId}`);

    // Disable server-side watcher on cleanup (glyph deletion)
    storeCleanup(element, () => {
        if (connectivity.state === 'online') {
            sendMessage({
                type: 'watcher_upsert',
                watcher_id: `ax-glyph-${glyphId}`,
                watcher_query: currentQuery,
                watcher_name: `AX Glyph: ${currentQuery.substring(0, 30)}`,
                enabled: false
            });
        }
    });

    // Subscribe to sync state changes for visual feedback
    const syncUnsub = syncStateManager.subscribe(glyphId, (state) => {
        element.dataset.syncState = state;
    });
    storeCleanup(element, syncUnsub);

    // Subscribe to connectivity state changes — re-fire local query on transition
    const connectUnsub = connectivity.subscribe((state) => {
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
    const text = renderTriple(attestation, {
        tag: 'div',
        fontSize: '11px',
        palette: { value: '#d4f0d4', keyword: '#6b7b6b' },
    });

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
        log.debug(SEG.GLYPH, `[AxGlyph] Cannot update results: glyph ${glyphId} not found in DOM`);
        return;
    }

    const resultsContainer = glyph.querySelector('.ax-glyph-results') as HTMLElement;
    if (!resultsContainer) {
        log.debug(SEG.GLYPH, `[AxGlyph] Cannot update results: results container not found for ${glyphId}`);
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

    // Type attestations: group by subject into a single line
    if (isTypeAttestation(attestation)) {
        const subject = attestation.subjects?.[0] || '';
        const existing = resultsContainer.querySelector(`[data-type-subject="${subject}"]`) as HTMLElement | null;
        if (existing) {
            // Merge: update attestation count in the existing group line
            const stored = JSON.parse(existing.dataset.typeAttestations || '[]') as Attestation[];
            stored.push(attestation);
            existing.dataset.typeAttestations = JSON.stringify(stored);
            const group = groupTypeAttestations(stored);
            if (group.length > 0) {
                const replacement = renderTypeResultLine(group[0]);
                replacement.dataset.typeSubject = subject;
                replacement.dataset.typeAttestations = JSON.stringify(stored);
                existing.replaceWith(replacement);
            }
        } else {
            const group = groupTypeAttestations([attestation]);
            if (group.length > 0) {
                const line = renderTypeResultLine(group[0]);
                line.dataset.typeSubject = subject;
                line.dataset.typeAttestations = JSON.stringify([attestation]);
                resultsContainer.insertBefore(line, resultsContainer.firstChild);
            }
        }
        log.debug(SEG.GLYPH, `[AxGlyph] Added type result to ${glyphId}: ${subject}`);
        return;
    }

    // Sigma attestations render directly
    if (isSigmaAttestation(attestation)) {
        resultsContainer.insertBefore(renderSigmaResultLine(attestation), resultsContainer.firstChild);
        log.debug(SEG.GLYPH, `[AxGlyph] Added sigma result to ${glyphId}:`, attestation.id);
        return;
    }

    // Regular attestation: check if a triplet group for this key already exists
    const key = tripletKey(attestation);
    const existingTriplet = resultsContainer.querySelector(`[data-triplet-key="${key}"]`) as HTMLElement | null;
    if (existingTriplet) {
        // Merge into existing triplet group
        const stored = JSON.parse(existingTriplet.dataset.tripletAttestations || '[]') as Attestation[];
        stored.push(attestation);
        const replacement = renderTripletResultLine(stored);
        replacement.dataset.tripletKey = key;
        replacement.dataset.tripletAttestations = JSON.stringify(stored);
        existingTriplet.replaceWith(replacement);
    } else {
        // First attestation with this key — render as single row, tag with triplet key for future merging
        const resultItem = renderAttestation(attestation);
        resultItem.dataset.tripletKey = key;
        resultItem.dataset.tripletAttestations = JSON.stringify([attestation]);
        resultsContainer.insertBefore(resultItem, resultsContainer.firstChild);
    }

    log.debug(SEG.GLYPH, `[AxGlyph] Added result to ${glyphId}:`, attestation.id);
}

/**
 * Update AX glyph with error message and optional structured details
 * Called by WebSocket handler when watcher_error message arrives
 */
export function updateAxGlyphError(glyphId: string, errorMsg: string, severity: string, details?: string[]): void {
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

    showQueryError(glyph, resultsContainer, 'ax-glyph-empty-state', 'ax-glyph-error', severity, errorMsg, 'AxGlyph', glyphId, details);
}
