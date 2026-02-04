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
import { GRID_SIZE } from './grid-constants';
import { makeDraggable, makeResizable } from './glyph-interaction';
import { sendMessage } from '../../websocket';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { getItem, setItem } from '../../state/storage';

/**
 * Factory function to create an Ax query editor glyph
 *
 * @param id Optional glyph ID
 * @param initialQuery Optional initial query text
 * @param gridX Optional grid X position
 * @param gridY Optional grid Y position
 */
/**
 * IndexedDB key prefix for ax query persistence
 */
const QUERY_STORAGE_KEY = 'qntx-ax-query:';

/**
 * Load persisted query from IndexedDB
 */
function loadQuery(id: string): string {
    try {
        return getItem<string>(QUERY_STORAGE_KEY + id) || '';
    } catch (error) {
        log.error(SEG.UI, `[AxGlyph] Failed to load query for ${id}:`, error);
        return '';
    }
}

/**
 * Save query to IndexedDB
 */
function saveQuery(id: string, query: string): void {
    try {
        setItem(QUERY_STORAGE_KEY + id, query);
        log.debug(SEG.UI, `[AxGlyph] Saved query for ${id} (${query.length} chars)`);
    } catch (error) {
        log.error(SEG.UI, `[AxGlyph] Failed to save query for ${id}:`, error);
    }
}

export function createAxGlyph(id?: string, initialQuery: string = '', gridX?: number, gridY?: number): Glyph {
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
        gridX,
        gridY,
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

            // Style element - resizable
            container.style.position = 'absolute';
            container.style.left = `${(glyph.gridX ?? gridX ?? 5) * GRID_SIZE}px`;
            container.style.top = `${(glyph.gridY ?? gridY ?? 5) * GRID_SIZE}px`;
            container.style.width = `${width}px`;
            container.style.height = `${height}px`;
            container.style.minWidth = '200px';
            container.style.minHeight = '120px';
            container.style.backgroundColor = 'var(--bg-secondary)'; // Will be updated based on watcher state
            container.style.borderRadius = '4px';
            container.style.border = '1px solid var(--border-color)';
            container.style.display = 'flex';
            container.style.flexDirection = 'column';
            container.style.overflow = 'hidden';
            container.style.zIndex = '1';

            // Title bar for dragging
            const titleBar = document.createElement('div');
            titleBar.className = 'ax-glyph-title-bar';
            titleBar.style.padding = '8px';
            titleBar.style.backgroundColor = 'var(--bg-tertiary)';
            titleBar.style.cursor = 'move';
            titleBar.style.userSelect = 'none';
            titleBar.style.fontWeight = 'bold';
            titleBar.style.fontSize = '14px';
            titleBar.style.display = 'flex';
            titleBar.style.alignItems = 'center';
            titleBar.style.justifyContent = 'space-between';

            // Label
            const label = document.createElement('span');
            label.textContent = AX;
            titleBar.appendChild(label);

            container.appendChild(titleBar);

            // Editor container
            const editorContainer = document.createElement('div');
            editorContainer.className = 'ax-glyph-editor';
            editorContainer.style.flex = '1';
            editorContainer.style.overflow = 'hidden';
            editorContainer.style.display = 'flex';
            editorContainer.style.flexDirection = 'column';

            // Text editor for the ax query
            const editor = document.createElement('textarea');
            editor.className = 'ax-query-textarea';
            editor.value = currentQuery;
            editor.placeholder = 'Enter ax query (e.g., is git, has certification)';
            editor.style.flex = '1';
            editor.style.width = '100%';
            editor.style.padding = '8px';
            editor.style.fontSize = '13px';
            editor.style.fontFamily = 'monospace';
            editor.style.border = 'none';
            editor.style.outline = 'none';
            editor.style.resize = 'none';
            editor.style.backgroundColor = 'var(--bg-primary)';
            editor.style.color = 'var(--text-primary)';
            editor.style.overflow = 'auto';

            // Auto-save and watcher update with debouncing (500ms delay)
            let saveTimeout: number | undefined;
            editor.addEventListener('input', () => {
                currentQuery = editor.value;

                // Clear existing timeout
                if (saveTimeout !== undefined) {
                    clearTimeout(saveTimeout);
                }

                // Update background to indicate pending state
                container.style.backgroundColor = '#2a2b3d'; // Slight blue tint for "updating"

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
                        container.style.backgroundColor = '#1f3d3d'; // Teal/cyan tint for "watching"

                        log.debug(SEG.UI, `[AxGlyph] Sent watcher upsert for ${glyphId}: "${currentQuery}"`);
                    } else {
                        // Empty query - revert to default
                        container.style.backgroundColor = 'var(--bg-secondary)';
                    }
                }, 500);
            });

            editorContainer.appendChild(editor);
            container.appendChild(editorContainer);

            // Results container - scrollable list of matched attestations
            const resultsContainer = document.createElement('div');
            resultsContainer.className = 'ax-glyph-results';
            resultsContainer.style.flex = '1';
            resultsContainer.style.overflow = 'auto';
            resultsContainer.style.padding = '8px';
            resultsContainer.style.backgroundColor = 'var(--bg-primary)';
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
                container.style.backgroundColor = '#1f3d3d'; // Teal/cyan tint for "watching"

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

            // Make draggable and resizable
            makeDraggable(container, titleBar, glyph, { logLabel: 'AxGlyph' });
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
    item.className = 'ax-glyph-result-item';
    item.style.padding = '8px';
    item.style.marginBottom = '4px';
    item.style.backgroundColor = 'var(--bg-secondary)';
    item.style.borderRadius = '2px';
    item.style.borderLeft = '3px solid var(--accent-color)';

    // Format attestation data (show key fields)
    const subjects = attestation.subjects?.join(', ') || 'N/A';
    const predicates = attestation.predicates?.join(', ') || 'N/A';
    const contexts = attestation.contexts?.join(', ') || 'N/A';

    item.innerHTML = `
        <div style="font-weight: bold; margin-bottom: 4px;">${attestation.id?.substring(0, 8) || 'unknown'}</div>
        <div style="font-size: 11px; color: var(--text-secondary);">
            <div>Subjects: ${subjects}</div>
            <div>Predicates: ${predicates}</div>
            <div>Contexts: ${contexts}</div>
        </div>
    `;

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
 * Update AX glyph with error message
 * Called by WebSocket handler when watcher_error message arrives
 */
export function updateAxGlyphError(glyphId: string, errorMsg: string, severity: string): void {
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

    // Create error display
    const errorDisplay = document.createElement('div');
    errorDisplay.className = 'ax-glyph-error';
    errorDisplay.style.padding = '12px';
    errorDisplay.style.marginBottom = '8px';
    errorDisplay.style.backgroundColor = severity === 'error' ? '#3d1f1f' : '#3d3d1f'; // Red for error, yellow for warning
    errorDisplay.style.borderLeft = `3px solid ${severity === 'error' ? '#ff4444' : '#ffaa00'}`;
    errorDisplay.style.borderRadius = '2px';
    errorDisplay.style.fontSize = '12px';
    errorDisplay.style.fontFamily = 'monospace';
    errorDisplay.style.color = 'var(--text-primary)';

    const severityLabel = document.createElement('div');
    severityLabel.textContent = severity.toUpperCase();
    severityLabel.style.fontWeight = 'bold';
    severityLabel.style.marginBottom = '4px';
    severityLabel.style.color = severity === 'error' ? '#ff4444' : '#ffaa00';

    const errorText = document.createElement('div');
    errorText.textContent = errorMsg;
    errorText.style.color = 'var(--text-secondary)';

    errorDisplay.appendChild(severityLabel);
    errorDisplay.appendChild(errorText);

    // Add error display at top of results
    resultsContainer.insertBefore(errorDisplay, resultsContainer.firstChild);

    // Update glyph background to indicate error state
    const container = glyph.closest('.canvas-ax-glyph') as HTMLElement;
    if (container) {
        container.style.backgroundColor = severity === 'error' ? '#3d1f1f' : '#3d3d1f'; // Red tint for error, yellow for warning
    }

    log.debug(SEG.UI, `[AxGlyph] Displayed ${severity} for ${glyphId}:`, errorMsg);
}

