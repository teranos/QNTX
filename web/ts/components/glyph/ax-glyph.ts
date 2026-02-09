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
import { applyCanvasGlyphLayout, makeDraggable, makeResizable, cleanupResizeObserver } from './glyph-interaction';
import { sendMessage } from '../../websocket';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { queryAttestations, parseQuery } from '../../qntx-wasm';
import { tooltip } from '../tooltip';
import { syncStateManager } from '../../state/sync-state';
import { connectivityManager } from '../../connectivity';
import {
    CANVAS_GLYPH_TITLE_BAR_HEIGHT,
    MAX_VIEWPORT_HEIGHT_RATIO
} from './glyph';

/**
 * Factory function to create an Ax query editor glyph
 *
 * @param id Optional glyph ID
 * @param initialQuery Optional initial query text
 * @param x Optional X position in pixels
 * @param y Optional Y position in pixels
 */
/**
 * LocalStorage key prefix for ax query persistence
 */
const QUERY_STORAGE_KEY = 'qntx-ax-query:';

/**
 * Load persisted query from localStorage
 */
function loadQuery(id: string): string {
    try {
        return localStorage.getItem(QUERY_STORAGE_KEY + id) || '';
    } catch (error) {
        log.error(SEG.GLYPH, `[AxGlyph] Failed to load query for ${id}:`, error);
        return '';
    }
}

/**
 * Save query to localStorage
 */
function saveQuery(id: string, query: string): void {
    try {
        localStorage.setItem(QUERY_STORAGE_KEY + id, query);
        log.debug(SEG.GLYPH, `[AxGlyph] Saved query for ${id} (${query.length} chars)`);
    } catch (error) {
        log.error(SEG.GLYPH, `[AxGlyph] Failed to save query for ${id}:`, error);
    }
}

export function createAxGlyph(id?: string, initialQuery: string = '', x?: number, y?: number): Glyph {
    const glyphId = id || `ax-${crypto.randomUUID()}`;

    // Load persisted query if available, otherwise use initialQuery
    const persistedQuery = loadQuery(glyphId);
    let currentQuery = persistedQuery || initialQuery;

    // Store matched attestations (will be populated by WebSocket handler)
    const matchedAttestations: Attestation[] = [];

    const glyph: Glyph = {
        id: glyphId,
        title: 'Ax Query',
        symbol: AX,
        manifestationType: 'ax',
        x,
        y,
        renderContent: () => {
            // Calculate default size
            const defaultWidth = 400;
            const defaultHeight = 200;
            const width = glyph.width ?? defaultWidth;
            const height = glyph.height ?? defaultHeight;

            // Main container
            const container = document.createElement('div');
            container.className = 'canvas-ax-glyph canvas-glyph';
            container.dataset.glyphId = glyphId;
            container.dataset.glyphSymbol = AX;

            // Style element - resizable
            applyCanvasGlyphLayout(container, { x: glyph.x ?? x ?? 200, y: glyph.y ?? y ?? 200, width, height });
            container.style.minWidth = '200px';
            container.style.minHeight = '120px';
            container.style.backgroundColor = 'rgba(30, 30, 35, 0.92)';
            container.style.zIndex = '1';

            // Title bar with inline query input
            const titleBar = document.createElement('div');
            titleBar.className = 'ax-glyph-title-bar';
            titleBar.style.padding = '4px 4px 4px 8px'; // Reduced top/bottom/right, keep left for symbol
            titleBar.style.backgroundColor = 'var(--bg-tertiary)';
            titleBar.style.userSelect = 'none';
            titleBar.style.fontSize = '14px';
            titleBar.style.display = 'flex';
            titleBar.style.alignItems = 'center';
            titleBar.style.gap = '8px';

            // Symbol (draggable area)
            const label = document.createElement('span');
            label.textContent = AX;
            label.style.cursor = 'move';
            label.style.fontWeight = 'bold';
            label.style.flexShrink = '0';
            label.style.color = 'var(--glyph-status-running-text)';
            titleBar.appendChild(label);

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

            titleBar.appendChild(editor);
            container.appendChild(titleBar);

            // Auto-save and watcher update with debouncing (500ms delay)
            let saveTimeout: number | undefined;
            editor.addEventListener('input', () => {
                currentQuery = editor.value;

                // Clear existing timeout
                if (saveTimeout !== undefined) {
                    clearTimeout(saveTimeout);
                }

                // Update background to indicate pending state
                container.style.backgroundColor = 'rgba(42, 43, 61, 0.92)'; // Slight blue tint for "updating"

                // Clear results immediately when query changes
                resultsContainer.innerHTML = '';
                const emptyState = document.createElement('div');
                emptyState.className = 'ax-glyph-empty-state';
                emptyState.textContent = 'No matches yet';
                emptyState.style.color = 'var(--text-secondary)';
                emptyState.style.textAlign = 'center';
                emptyState.style.padding = '20px';
                resultsContainer.appendChild(emptyState);

                // Debounce save and watcher update for 500ms
                saveTimeout = window.setTimeout(async () => {
                    saveQuery(glyphId, currentQuery);

                    if (!currentQuery.trim()) {
                        container.style.backgroundColor = 'rgba(30, 30, 35, 0.92)';
                        return;
                    }

                    // Local IndexedDB query — immediate results, no server round-trip
                    try {
                        const parsed = parseQuery(currentQuery.trim());
                        if (parsed.ok) {
                            const localResults = await queryAttestations(parsed.query);
                            if (localResults.length > 0) {
                                const empty = resultsContainer.querySelector('.ax-glyph-empty-state');
                                if (empty) empty.remove();

                                const displayedIds = new Set<string>();
                                for (const att of localResults) {
                                    if (att.id) displayedIds.add(att.id);
                                    resultsContainer.appendChild(renderAttestation(att));
                                }
                                (container as any)._localIds = displayedIds;

                                log.debug(SEG.GLYPH, `[AxGlyph] Local query: ${localResults.length} results for ${glyphId}`);
                            }
                        }
                    } catch (err) {
                        log.debug(SEG.GLYPH, `[AxGlyph] Local query failed for ${glyphId}:`, err);
                    }

                    // Orange = local/WASM results, teal = server watcher active
                    container.style.backgroundColor = 'rgba(61, 45, 20, 0.92)'; // Orange: local-only

                    // Send watcher upsert via WebSocket when online (server supplements local)
                    if (connectivityManager.state === 'online') {
                        sendMessage({
                            type: 'watcher_upsert',
                            watcher_id: `ax-glyph-${glyphId}`,
                            watcher_query: currentQuery.trim(),
                            watcher_name: `AX Glyph: ${currentQuery.substring(0, 30)}${currentQuery.length > 30 ? '...' : ''}`,
                            enabled: true
                        });
                        // Shift to teal once server watcher is active
                        container.style.backgroundColor = 'rgba(31, 61, 61, 0.92)';
                    }

                    log.debug(SEG.GLYPH, `[AxGlyph] Query updated for ${glyphId}: "${currentQuery}"`);
                }, 500);
            });

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

            // Initial empty state
            const emptyState = document.createElement('div');
            emptyState.className = 'ax-glyph-empty-state';
            emptyState.textContent = 'No matches yet';
            emptyState.style.color = 'var(--text-secondary)';
            emptyState.style.textAlign = 'center';
            emptyState.style.padding = '20px';
            resultsContainer.appendChild(emptyState);

            container.appendChild(resultsContainer);

            // Store reference to results container on glyph for WebSocket handler access
            (glyph as any).resultsContainer = resultsContainer;
            (glyph as any).matchedAttestations = matchedAttestations;

            // Attach tooltip support for attestation results
            tooltip.attach(resultsContainer, '.ax-glyph-result-item');

            // If we loaded a persisted query, run local + server query
            if (currentQuery.trim()) {
                // Local query first
                (async () => {
                    try {
                        const parsed = parseQuery(currentQuery.trim());
                        if (parsed.ok) {
                            const localResults = await queryAttestations(parsed.query);
                            if (localResults.length > 0) {
                                const empty = resultsContainer.querySelector('.ax-glyph-empty-state');
                                if (empty) empty.remove();
                                const displayedIds = new Set<string>();
                                for (const att of localResults) {
                                    if (att.id) displayedIds.add(att.id);
                                    resultsContainer.appendChild(renderAttestation(att));
                                }
                                (container as any)._localIds = displayedIds;
                            }
                        }
                    } catch { /* WASM not ready yet — server will provide results */ }
                })();

                // Orange = local-only, teal = server active
                container.style.backgroundColor = 'rgba(61, 45, 20, 0.92)';

                if (connectivityManager.state === 'online') {
                    sendMessage({
                        type: 'watcher_upsert',
                        watcher_id: `ax-glyph-${glyphId}`,
                        watcher_query: currentQuery.trim(),
                        watcher_name: `AX Glyph: ${currentQuery.substring(0, 30)}${currentQuery.length > 30 ? '...' : ''}`,
                        enabled: true
                    });
                    container.style.backgroundColor = 'rgba(31, 61, 61, 0.92)'; // Teal: server watcher active
                }

                log.debug(SEG.GLYPH, `[AxGlyph] Restored query for ${glyphId}: "${currentQuery}"`);
            }

            // Resize handle
            const resizeHandle = document.createElement('div');
            resizeHandle.className = 'ax-glyph-resize-handle glyph-resize-handle';
            container.appendChild(resizeHandle);

            // Make draggable and resizable (drag via symbol only)
            makeDraggable(container, label, glyph, { logLabel: 'AxGlyph' });
            makeResizable(container, resizeHandle, glyph, { logLabel: 'AxGlyph' });

            // Set up ResizeObserver for auto-sizing glyph to content
            setupAxGlyphResizeObserver(container, resultsContainer, glyphId);

            // Subscribe to sync state changes for visual feedback
            syncStateManager.subscribe(glyphId, (state) => {
                container.dataset.syncState = state;
            });

            // Subscribe to connectivity state changes
            connectivityManager.subscribe((state) => {
                container.dataset.connectivityMode = state;
                resultsContainer.dataset.connectivityMode = state;
            });

            return container;
        }
    };

    return glyph;
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
    item.style.cursor = 'default';

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

    // Build formatted text with darker keywords
    text.innerHTML = `<span style="color: #d4f0d4;">${subjects}</span> <span style="color: #6b7b6b;">is</span> <span style="color: #d4f0d4;">${predicates}</span> <span style="color: #6b7b6b;">of</span> <span style="color: #d4f0d4;">${contexts}</span>`;

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

    // Update glyph background to indicate error state
    const container = glyph.closest('.canvas-ax-glyph') as HTMLElement;
    if (container) {
        container.style.backgroundColor = severity === 'error' ? 'rgba(61, 31, 31, 0.92)' : 'rgba(61, 61, 31, 0.92)'; // Red tint for error, yellow for warning
    }

    log.debug(SEG.GLYPH, `[AxGlyph] Displayed ${severity} for ${glyphId}:`, errorMsg);
}

