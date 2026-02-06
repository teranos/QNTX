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
import { makeDraggable, makeResizable } from './glyph-interaction';
import { sendMessage } from '../../websocket';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { tooltip } from '../tooltip';

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
        log.error(SEG.UI, `[AxGlyph] Failed to load query for ${id}:`, error);
        return '';
    }
}

/**
 * Save query to localStorage
 */
function saveQuery(id: string, query: string): void {
    try {
        localStorage.setItem(QUERY_STORAGE_KEY + id, query);
        log.debug(SEG.UI, `[AxGlyph] Saved query for ${id} (${query.length} chars)`);
    } catch (error) {
        log.error(SEG.UI, `[AxGlyph] Failed to save query for ${id}:`, error);
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
            container.className = 'canvas-ax-glyph';
            container.dataset.glyphId = glyphId;
            container.dataset.glyphSymbol = AX;

            // Style element - resizable
            container.style.position = 'absolute';
            container.style.left = `${glyph.x ?? x ?? 200}px`;
            container.style.top = `${glyph.y ?? y ?? 200}px`;
            container.style.width = `${width}px`;
            container.style.height = `${height}px`;
            container.style.minWidth = '200px';
            container.style.minHeight = '120px';
            container.style.backgroundColor = 'rgba(30, 30, 35, 0.92)'; // Darker with transparency
            container.style.borderRadius = '4px';
            container.style.border = '1px solid var(--border-color)';
            container.style.display = 'flex';
            container.style.flexDirection = 'column';
            container.style.overflow = 'hidden';
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
            label.style.color = '#6b9bd1'; // Azure-ish blue
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
                saveTimeout = window.setTimeout(() => {
                    saveQuery(glyphId, currentQuery);

                    // Send watcher upsert via WebSocket
                    if (currentQuery.trim()) {
                        sendMessage({
                            type: 'watcher_upsert',
                            watcher_id: `ax-glyph-${glyphId}`,
                            watcher_query: currentQuery.trim(),
                            watcher_name: `AX Glyph: ${currentQuery.substring(0, 30)}${currentQuery.length > 30 ? '...' : ''}`,
                            enabled: true
                        });

                        // Update background to indicate active watcher
                        container.style.backgroundColor = 'rgba(31, 61, 61, 0.92)'; // Teal/cyan tint for "watching"

                        log.debug(SEG.UI, `[AxGlyph] Sent watcher upsert for ${glyphId}: "${currentQuery}"`);
                    } else {
                        // Empty query - revert to default
                        container.style.backgroundColor = 'rgba(30, 30, 35, 0.92)';
                    }
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

            // If we loaded a persisted query, send watcher_upsert to activate it
            if (currentQuery.trim()) {
                sendMessage({
                    type: 'watcher_upsert',
                    watcher_id: `ax-glyph-${glyphId}`,
                    watcher_query: currentQuery.trim(),
                    watcher_name: `AX Glyph: ${currentQuery.substring(0, 30)}${currentQuery.length > 30 ? '...' : ''}`,
                    enabled: true
                });

                // Update background to show active watcher state
                container.style.backgroundColor = 'rgba(31, 61, 61, 0.92)'; // Teal/cyan tint for "watching"

                log.debug(SEG.UI, `[AxGlyph] Restored and activated watcher for ${glyphId}: "${currentQuery}"`);
            }

            // Resize handle
            const resizeHandle = document.createElement('div');
            resizeHandle.className = 'ax-glyph-resize-handle';
            resizeHandle.style.position = 'absolute';
            resizeHandle.style.bottom = '0';
            resizeHandle.style.right = '0';
            resizeHandle.style.width = '16px';
            resizeHandle.style.height = '16px';
            resizeHandle.style.cursor = 'nwse-resize';
            resizeHandle.style.backgroundColor = 'var(--bg-tertiary)';
            resizeHandle.style.borderTopLeftRadius = '4px';
            container.appendChild(resizeHandle);

            // Make draggable and resizable (drag via symbol only)
            makeDraggable(container, label, glyph, { logLabel: 'AxGlyph' });
            makeResizable(container, resizeHandle, glyph, { logLabel: 'AxGlyph' });

            return container;
        }
    };

    return glyph;
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
        log.warn(SEG.UI, `[AxGlyph] Cannot update results: glyph ${glyphId} not found in DOM`);
        return;
    }

    const resultsContainer = glyph.querySelector('.ax-glyph-results') as HTMLElement;
    if (!resultsContainer) {
        log.warn(SEG.UI, `[AxGlyph] Cannot update results: results container not found for ${glyphId}`);
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

    // Add new result at top (most recent first)
    const resultItem = renderAttestation(attestation);
    resultsContainer.insertBefore(resultItem, resultsContainer.firstChild);

    log.debug(SEG.UI, `[AxGlyph] Added result to ${glyphId}:`, attestation.id);
}

/**
 * Update AX glyph with error message and optional structured details
 * Called by WebSocket handler when watcher_error message arrives
 */
export function updateAxGlyphError(glyphId: string, errorMsg: string, severity: string, details?: string[]): void {
    // Find the glyph element by data attribute
    const glyph = document.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement;
    if (!glyph) {
        log.warn(SEG.UI, `[AxGlyph] Cannot update error: glyph ${glyphId} not found in DOM`);
        return;
    }

    const resultsContainer = glyph.querySelector('.ax-glyph-results') as HTMLElement;
    if (!resultsContainer) {
        log.warn(SEG.UI, `[AxGlyph] Cannot update error: results container not found for ${glyphId}`);
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
    errorDisplay.style.backgroundColor = severity === 'error' ? '#2b1a1a' : '#2b2b1a';
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

    log.debug(SEG.UI, `[AxGlyph] Displayed ${severity} for ${glyphId}:`, errorMsg);
}

