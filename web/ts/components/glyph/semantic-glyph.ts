/**
 * Semantic Search Glyph (⊨) — natural language query on canvas
 *
 * Structurally identical to AX glyph: query input → watcher on backend → live results.
 * Difference: AX sends structured AX query syntax; SE sends natural language matched
 * by cosine similarity against attestation embeddings on the server.
 *
 * Offline fallback: when disconnected, uses cached query embedding + local IndexedDB
 * embeddings for brute-force cosine similarity search via WASM.
 */

import type { Glyph } from './glyph';
import { SE } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { preventDrag, storeCleanup, cleanupResizeObserver } from './glyph-interaction';
import { canvasPlaced } from './manifestations/canvas-placed';
import { sendMessage } from '../../websocket';
import { apiFetch } from '../../api';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { tooltip } from '../tooltip';
import { spawnAttestationGlyph } from './attestation-glyph';
import { uiState } from '../../state/ui';
import { syncStateManager } from '../../state/sync-state';
import { connectivityManager } from '../../connectivity';
import {
    CANVAS_GLYPH_TITLE_BAR_HEIGHT,
    MAX_VIEWPORT_HEIGHT_RATIO
} from './glyph';

// ============================================================================
// Query Embedding Cache (for offline fallback)
// ============================================================================

/** Cached query embeddings keyed by watcher ID */
const queryEmbeddingCache = new Map<string, number[]>();

/**
 * Cache a query embedding received from the server.
 * Persists to IndexedDB so it survives page reloads and timing gaps.
 * Called by the WebSocket handler for se_query_embedding messages.
 */
export function cacheQueryEmbedding(watcherId: string, embedding: number[]): void {
    queryEmbeddingCache.set(watcherId, embedding);
    // Persist to IndexedDB (fire-and-forget)
    import('../../browser-sync.js').then(({ storeQueryEmbedding }) => {
        storeQueryEmbedding(watcherId, embedding).catch(() => {});
    });
    log.debug(SEG.GLYPH, `[SeGlyph] Cached query embedding for ${watcherId} (${embedding.length}d)`);
}

/**
 * Run local semantic search using cached query embedding and IndexedDB embeddings.
 * Returns attestations from local storage that match above threshold.
 */
async function runLocalSemanticSearch(
    glyphId: string,
    watcherId: string,
    threshold: number,
): Promise<void> {
    // Try in-memory cache first, then IndexedDB
    let queryEmb = queryEmbeddingCache.get(watcherId);
    if (!queryEmb) {
        try {
            const { getQueryEmbedding } = await import('../../browser-sync.js');
            queryEmb = await getQueryEmbedding(watcherId) ?? undefined;
            if (queryEmb) {
                queryEmbeddingCache.set(watcherId, queryEmb);
                log.debug(SEG.GLYPH, `[SeGlyph] Loaded query embedding from IndexedDB for ${watcherId}`);
            }
        } catch { /* IndexedDB unavailable */ }
    }
    if (!queryEmb) {
        log.debug(SEG.GLYPH, `[SeGlyph] No cached query embedding for ${watcherId}, cannot search offline`);
        return;
    }

    try {
        const { localSemanticSearch } = await import('../../local-semantic-search.js');
        const { getAttestation } = await import('../../qntx-wasm.js');
        const results = await localSemanticSearch(queryEmb, threshold, 50);

        for (const match of results) {
            const att = await getAttestation(match.attestation_id);
            if (att) {
                updateSemanticGlyphResults(glyphId, att, match.similarity);
            }
        }

        if (results.length > 0) {
            log.info(SEG.GLYPH, `[SeGlyph] Local search for ${glyphId}: ${results.length} results`);
        }
    } catch (err) {
        log.debug(SEG.GLYPH, `[SeGlyph] Local search failed for ${glyphId}:`, err);
    }
}

function appendEmptyState(container: HTMLElement): void {
    const empty = document.createElement('div');
    empty.className = 'se-glyph-empty-state';
    empty.textContent = 'No matches yet';
    empty.style.color = 'var(--text-secondary)';
    empty.style.textAlign = 'center';
    empty.style.padding = '20px';
    container.appendChild(empty);
}

/**
 * Create a Semantic Search glyph using canvasPlaced() with custom title bar.
 *
 * @param glyph - Glyph model with id, position, and size
 * @returns The canvas-placed HTMLElement
 */
export function createSemanticGlyph(glyph: Glyph): HTMLElement {
    const glyphId = glyph.id;

    // Load persisted state from canvas (query + threshold stored as JSON)
    const existingGlyph = uiState.getCanvasGlyphs().find(g => g.id === glyphId);
    let currentQuery = '';
    let currentThreshold = 0.5;
    let currentClusterId: number | null = null;
    if (existingGlyph?.content) {
        try {
            const parsed = JSON.parse(existingGlyph.content);
            currentQuery = parsed.query ?? '';
            currentThreshold = parsed.threshold ?? 0.5;
            currentClusterId = parsed.clusterId ?? null;
        } catch (err) {
            log.debug(SEG.GLYPH, `[SeGlyph] Failed to parse persisted content as JSON for ${glyphId}, treating as legacy string:`, err);
            currentQuery = existingGlyph.content;
        }
    } else if (glyph.content) {
        try {
            const parsed = JSON.parse(glyph.content);
            currentQuery = parsed.query ?? '';
            currentThreshold = parsed.threshold ?? 0.5;
            currentClusterId = parsed.clusterId ?? null;
        } catch (err) {
            log.debug(SEG.GLYPH, `[SeGlyph] Failed to parse glyph content as JSON for ${glyphId}, treating as legacy string:`, err);
            currentQuery = glyph.content;
        }
    }

    // Symbol (draggable area)
    const symbol = document.createElement('span');
    symbol.textContent = SE;
    symbol.style.cursor = 'move';
    symbol.style.fontWeight = 'bold';
    symbol.style.flexShrink = '0';
    symbol.style.color = 'var(--glyph-status-running-text)';

    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-se-glyph',
        defaults: { x: 200, y: 200, width: 400, height: 200 },
        dragHandle: symbol,
        resizable: true,
        logLabel: 'SeGlyph',
    });
    element.style.minWidth = '200px';
    element.style.minHeight = '120px';

    // Query input
    const editor = document.createElement('input');
    editor.type = 'text';
    editor.className = 'se-query-input';
    editor.value = currentQuery;
    editor.placeholder = 'Semantic query (natural language)';
    editor.style.flex = '1';
    editor.style.padding = '4px 8px';
    editor.style.fontSize = '13px';
    editor.style.fontFamily = 'monospace';
    editor.style.border = 'none';
    editor.style.outline = 'none';
    editor.style.backgroundColor = 'rgba(25, 25, 30, 0.95)';
    editor.style.color = '#d4f0d4';
    editor.style.borderRadius = '2px';

    preventDrag(editor);

    // Cluster scope dropdown
    const clusterSelect = document.createElement('select');
    clusterSelect.className = 'se-cluster-select';
    clusterSelect.style.padding = '2px 4px';
    clusterSelect.style.fontSize = '11px';
    clusterSelect.style.fontFamily = 'monospace';
    clusterSelect.style.border = 'none';
    clusterSelect.style.outline = 'none';
    clusterSelect.style.backgroundColor = 'rgba(25, 25, 30, 0.95)';
    clusterSelect.style.color = 'var(--text-secondary)';
    clusterSelect.style.borderRadius = '2px';
    clusterSelect.style.cursor = 'pointer';
    clusterSelect.style.flexShrink = '0';

    const allOption = document.createElement('option');
    allOption.value = '';
    allOption.textContent = 'All';
    clusterSelect.appendChild(allOption);

    // Track whether embedding service is available
    let embeddingsAvailable = true;

    // Fetch embedding service status and clusters
    apiFetch('/api/embeddings/info')
        .then(r => r.json())
        .then(info => {
            if (!info.available) {
                embeddingsAvailable = false;
                editor.disabled = true;
                editor.placeholder = 'Embedding service unavailable';
                thresholdSlider.disabled = true;
                clusterSelect.disabled = true;
                setColorState('idle');

                // Show unavailable message in results area
                resultsContainer.innerHTML = '';
                const notice = document.createElement('div');
                notice.className = 'se-glyph-unavailable';
                notice.style.padding = '16px';
                notice.style.color = 'var(--text-secondary)';
                notice.style.fontSize = '12px';
                notice.style.fontFamily = 'monospace';
                notice.style.textAlign = 'center';
                notice.style.lineHeight = '1.6';
                notice.innerHTML = `Embedding service is not enabled.<br><br>`
                    + `Enable in <code style="color: #d4f0d4;">am.toml</code>:<br>`
                    + `<code style="color: #d4f0d4;">[embeddings]<br>enabled = true</code>`;
                resultsContainer.appendChild(notice);

                log.debug(SEG.GLYPH, `[SeGlyph] Embedding service unavailable for ${glyphId}`);
                return;
            }

            // Fetch full cluster details (with labels) for the selector
            apiFetch('/api/embeddings/clusters')
                .then(r => r.ok ? r.json() : [])
                .then((clusters: Array<{ id: number; label: string | null; members: number }>) => {
                    for (const c of clusters) {
                        const opt = document.createElement('option');
                        opt.value = String(c.id);
                        opt.textContent = c.label
                            ? `#${c.id} ${c.label} (${c.members})`
                            : `#${c.id} (${c.members})`;
                        clusterSelect.appendChild(opt);
                    }
                    if (currentClusterId !== null) {
                        clusterSelect.value = String(currentClusterId);
                    }
                })
                .catch(() => {
                    // Fallback to cluster_info from /info if /clusters fails
                    if (info.cluster_info?.clusters) {
                        const clusters = info.cluster_info.clusters as Record<string, number>;
                        for (const [id, count] of Object.entries(clusters)) {
                            if (parseInt(id) < 0) continue;
                            const opt = document.createElement('option');
                            opt.value = id;
                            opt.textContent = `#${id} (${count})`;
                            clusterSelect.appendChild(opt);
                        }
                        if (currentClusterId !== null) {
                            clusterSelect.value = String(currentClusterId);
                        }
                    }
                });
        })
        .catch(err => log.debug(SEG.GLYPH, `[SeGlyph] Failed to fetch embedding info:`, err));

    preventDrag(clusterSelect);

    // Threshold slider
    const thresholdContainer = document.createElement('div');
    thresholdContainer.style.display = 'flex';
    thresholdContainer.style.alignItems = 'center';
    thresholdContainer.style.gap = '4px';
    thresholdContainer.style.flexShrink = '0';

    const thresholdSlider = document.createElement('input');
    thresholdSlider.type = 'range';
    thresholdSlider.min = '0.1';
    thresholdSlider.max = '1.0';
    thresholdSlider.step = '0.05';
    thresholdSlider.value = String(currentThreshold);
    thresholdSlider.style.width = '60px';
    thresholdSlider.style.cursor = 'pointer';

    preventDrag(thresholdSlider);

    const thresholdLabel = document.createElement('span');
    thresholdLabel.style.fontSize = '11px';
    thresholdLabel.style.color = 'var(--text-secondary)';
    thresholdLabel.style.fontFamily = 'monospace';
    thresholdLabel.style.minWidth = '28px';
    thresholdLabel.textContent = currentThreshold.toFixed(2);

    thresholdContainer.appendChild(thresholdSlider);
    thresholdContainer.appendChild(thresholdLabel);

    // Title bar (symbol + query input + threshold)
    const titleBar = document.createElement('div');
    titleBar.className = 'canvas-glyph-title-bar';
    titleBar.style.padding = '4px 4px 4px 8px';

    titleBar.appendChild(symbol);
    titleBar.appendChild(editor);
    titleBar.appendChild(clusterSelect);
    titleBar.appendChild(thresholdContainer);

    element.appendChild(titleBar);

    // Color states (same palette as AX)
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

    // Results container
    const resultsContainer = document.createElement('div');
    resultsContainer.className = 'se-glyph-results';
    resultsContainer.style.flex = '1';
    resultsContainer.style.overflow = 'auto';
    resultsContainer.style.padding = '8px';
    resultsContainer.style.backgroundColor = 'rgba(25, 25, 30, 0.95)';
    resultsContainer.style.borderTop = '1px solid var(--border-color)';
    resultsContainer.style.fontSize = '12px';
    resultsContainer.style.fontFamily = 'monospace';

    appendEmptyState(resultsContainer);

    element.appendChild(resultsContainer);

    // Tooltip support for attestation results
    tooltip.attach(resultsContainer, '.se-glyph-result-item');

    // Persist state and send watcher upsert to server
    function commitQuery(): void {
        if (!embeddingsAvailable) return;

        const query = currentQuery.trim();

        // Persist to canvas state
        const existing = uiState.getCanvasGlyphs().find(g => g.id === glyphId);
        if (existing) {
            uiState.addCanvasGlyph({ ...existing, content: JSON.stringify({ query: currentQuery, threshold: currentThreshold, clusterId: currentClusterId }) });
        }

        if (!query) {
            setColorState('idle');
            return;
        }

        const watcherId = `se-glyph-${glyphId}`;
        if (connectivityManager.state === 'online') {
            sendMessage({
                type: 'watcher_upsert',
                watcher_id: watcherId,
                semantic_query: query,
                semantic_threshold: currentThreshold,
                semantic_cluster_id: currentClusterId,
                watcher_name: `SE: ${query.substring(0, 30)}${query.length > 30 ? '...' : ''}`,
                enabled: true
            });
            setColorState('teal');
        } else {
            // Offline: run local semantic search using cached query embedding
            setColorState('orange');
            runLocalSemanticSearch(glyphId, watcherId, currentThreshold);
        }
    }

    // Debounced input handler (500ms)
    let saveTimeout: number | undefined;
    function handleInputChange(): void {
        if (saveTimeout !== undefined) {
            clearTimeout(saveTimeout);
        }

        setColorState('pending');

        resultsContainer.innerHTML = '';
        appendEmptyState(resultsContainer);

        saveTimeout = window.setTimeout(() => {
            commitQuery();
            log.debug(SEG.GLYPH, `[SeGlyph] Query updated for ${glyphId}: "${currentQuery}" threshold=${currentThreshold}`);
        }, 500);
    }

    editor.addEventListener('input', () => {
        currentQuery = editor.value;
        handleInputChange();
    });

    thresholdSlider.addEventListener('input', () => {
        currentThreshold = parseFloat(thresholdSlider.value);
        thresholdLabel.textContent = currentThreshold.toFixed(2);
        handleInputChange();
    });

    clusterSelect.addEventListener('change', () => {
        currentClusterId = clusterSelect.value ? parseInt(clusterSelect.value) : null;
        handleInputChange();
    });

    // ResizeObserver for auto-sizing
    setupSeGlyphResizeObserver(element, resultsContainer, glyphId);

    // Sync state subscription
    const syncUnsub = syncStateManager.subscribe(glyphId, (state) => {
        element.dataset.syncState = state;
    });
    storeCleanup(element, syncUnsub);

    // Connectivity subscription — fires immediately with current state (serves as
    // initial commitQuery for persisted queries) and on subsequent transitions.
    const connectUnsub = connectivityManager.subscribe((state) => {
        element.dataset.connectivityMode = state;
        resultsContainer.dataset.connectivityMode = state;

        if (currentQuery.trim()) {
            commitQuery();
        }
    });
    storeCleanup(element, connectUnsub);

    return element;
}

function setupSeGlyphResizeObserver(
    glyphElement: HTMLElement,
    resultsContainer: HTMLElement,
    glyphId: string
): void {
    cleanupResizeObserver(glyphElement, `SE ${glyphId}`);

    const titleBarHeight = CANVAS_GLYPH_TITLE_BAR_HEIGHT;
    const maxHeight = window.innerHeight * MAX_VIEWPORT_HEIGHT_RATIO;

    const resizeObserver = new ResizeObserver(entries => {
        for (const entry of entries) {
            const contentHeight = entry.contentRect.height;
            const totalHeight = Math.min(contentHeight + titleBarHeight, maxHeight);

            glyphElement.style.minHeight = `${totalHeight}px`;

            log.debug(SEG.GLYPH, `[SE ${glyphId}] Auto-resized to ${totalHeight}px (content: ${contentHeight}px)`);
        }
    });

    resizeObserver.observe(resultsContainer);

    (glyphElement as any).__resizeObserver = resizeObserver;
}

/**
 * Extract rich text from attestation attributes (string values),
 * falling back to structural fields if no attributes present.
 */
function extractRichText(attestation: Attestation): string {
    if (attestation.attributes) {
        try {
            const attrs = typeof attestation.attributes === 'string'
                ? JSON.parse(attestation.attributes)
                : attestation.attributes;
            const parts: string[] = [];
            for (const value of Object.values(attrs)) {
                if (typeof value === 'string' && value !== '') {
                    parts.push(value);
                } else if (Array.isArray(value)) {
                    for (const item of value) {
                        if (typeof item === 'string' && item !== '') {
                            parts.push(item);
                        }
                    }
                }
            }
            if (parts.length > 0) return parts.join(' ');
        } catch { /* ignore parse errors */ }
    }
    return '';
}

/**
 * Build tooltip showing attestation structure and which attribute fields matched.
 */
function buildAttestationTooltip(attestation: Attestation): string {
    const subjects = attestation.subjects?.join(', ') || 'N/A';
    const predicates = attestation.predicates?.join(', ') || 'N/A';
    const contexts = attestation.contexts?.join(', ') || 'N/A';

    const lines: string[] = [`${subjects} is ${predicates} of ${contexts}`];

    if (attestation.attributes) {
        try {
            const attrs = typeof attestation.attributes === 'string'
                ? JSON.parse(attestation.attributes)
                : attestation.attributes;
            for (const [key, value] of Object.entries(attrs)) {
                if (key === 'rich_string_fields') continue;
                if (typeof value === 'string' && value !== '') {
                    const truncated = value.length > 80 ? value.substring(0, 80) + '...' : value;
                    lines.push(`${key}: ${truncated}`);
                } else if (Array.isArray(value) && value.some(v => typeof v === 'string')) {
                    const strs = value.filter((v): v is string => typeof v === 'string' && v !== '');
                    if (strs.length > 0) {
                        const joined = strs.join(', ');
                        const truncated = joined.length > 80 ? joined.substring(0, 80) + '...' : joined;
                        lines.push(`${key}: ${truncated}`);
                    }
                }
            }
        } catch { /* ignore parse errors */ }
    }

    return lines.join('\n');
}

/**
 * Render a single attestation result with similarity score.
 * Shows rich text as primary display; attestation structure on hover.
 */
function renderAttestation(attestation: Attestation, score?: number): HTMLElement {
    const item = document.createElement('div');
    item.className = 'se-glyph-result-item has-tooltip';
    item.style.padding = '4px 8px';
    item.style.marginBottom = '2px';
    item.style.backgroundColor = 'rgba(31, 61, 31, 0.35)';
    item.style.borderRadius = '2px';
    item.style.cursor = 'pointer';
    item.style.display = 'flex';
    item.style.alignItems = 'center';
    item.style.gap = '8px';
    if (attestation.id) {
        item.dataset.attestationId = attestation.id;
    }
    if (score !== undefined) {
        item.dataset.score = String(score);
    }

    // Store full attestation for double-click spawn
    item.dataset.attestation = JSON.stringify(attestation);
    item.addEventListener('dblclick', (e) => {
        e.stopPropagation();
        spawnAttestationGlyph(attestation, e.clientX, e.clientY);
    });

    const text = document.createElement('div');
    text.style.fontSize = '11px';
    text.style.color = '#d4f0d4';
    text.style.fontFamily = 'monospace';
    text.style.whiteSpace = 'nowrap';
    text.style.overflow = 'hidden';
    text.style.textOverflow = 'ellipsis';
    text.style.flex = '1';
    text.textContent = extractRichText(attestation);

    item.dataset.tooltip = buildAttestationTooltip(attestation);
    item.appendChild(text);

    // Score badge (right-aligned)
    if (score !== undefined && score > 0) {
        const badge = document.createElement('span');
        badge.style.fontSize = '10px';
        badge.style.fontFamily = 'monospace';
        badge.style.color = 'var(--text-secondary)';
        badge.style.flexShrink = '0';
        badge.textContent = `${Math.round(score * 100)}%`;
        item.appendChild(badge);
    }

    return item;
}

/**
 * Update the results display with a new attestation match.
 * Results are sorted by similarity score (highest first).
 */
export function updateSemanticGlyphResults(glyphId: string, attestation: Attestation, score?: number): void {
    const glyph = document.querySelector(`[data-glyph-id="${glyphId}"]`);
    if (!glyph) {
        log.warn(SEG.GLYPH, `[SeGlyph] Cannot update results: glyph ${glyphId} not found in DOM`);
        return;
    }

    const resultsContainer = glyph.querySelector('.se-glyph-results') as HTMLElement;
    if (!resultsContainer) {
        log.warn(SEG.GLYPH, `[SeGlyph] Cannot update results: results container not found for ${glyphId}`);
        return;
    }

    // Remove empty state if present
    const emptyState = resultsContainer.querySelector('.se-glyph-empty-state');
    if (emptyState) {
        emptyState.remove();
    }

    // Remove error display if present (successful match clears error)
    const errorDisplay = resultsContainer.querySelector('.se-glyph-error');
    if (errorDisplay) {
        errorDisplay.remove();
    }

    // Dedup: skip if this attestation is already displayed
    if (attestation.id) {
        const existingItems = resultsContainer.querySelectorAll('.se-glyph-result-item');
        for (const existing of existingItems) {
            if ((existing as HTMLElement).dataset.attestationId === attestation.id) {
                log.debug(SEG.GLYPH, `[SeGlyph] Skipped duplicate ${attestation.id} for ${glyphId}`);
                return;
            }
        }
    }

    const resultItem = renderAttestation(attestation, score);

    // Insert sorted by score (highest first)
    const thisScore = score ?? 0;
    const existingItems = resultsContainer.querySelectorAll('.se-glyph-result-item');
    let inserted = false;
    for (const existing of existingItems) {
        const existingScore = parseFloat((existing as HTMLElement).dataset.score ?? '0');
        if (thisScore > existingScore) {
            resultsContainer.insertBefore(resultItem, existing);
            inserted = true;
            break;
        }
    }
    if (!inserted) {
        resultsContainer.appendChild(resultItem);
    }

    log.debug(SEG.GLYPH, `[SeGlyph] Added result to ${glyphId} (score=${score?.toFixed(2) ?? 'n/a'}):`, attestation.id);
}

/**
 * Update SE glyph with error message and optional structured details
 */
export function updateSemanticGlyphError(glyphId: string, errorMsg: string, severity: string, details?: string[]): void {
    const glyph = document.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement;
    if (!glyph) {
        log.warn(SEG.GLYPH, `[SeGlyph] Cannot update error: glyph ${glyphId} not found in DOM`);
        return;
    }

    const resultsContainer = glyph.querySelector('.se-glyph-results') as HTMLElement;
    if (!resultsContainer) {
        log.warn(SEG.GLYPH, `[SeGlyph] Cannot update error: results container not found for ${glyphId}`);
        return;
    }

    // Remove empty state if present
    const emptyState = resultsContainer.querySelector('.se-glyph-empty-state');
    if (emptyState) {
        emptyState.remove();
    }

    // Remove existing error display if present
    const existingError = resultsContainer.querySelector('.se-glyph-error');
    if (existingError) {
        existingError.remove();
    }

    // Create error display
    const errorDisplay = document.createElement('div');
    errorDisplay.className = 'se-glyph-error';
    errorDisplay.style.padding = '6px 8px';
    errorDisplay.style.fontSize = '11px';
    errorDisplay.style.fontFamily = 'monospace';
    errorDisplay.style.backgroundColor = severity === 'error' ? 'var(--glyph-status-error-section-bg)' : '#2b2b1a';
    errorDisplay.style.color = severity === 'error' ? '#ff9999' : '#ffcc66';
    errorDisplay.style.whiteSpace = 'pre-wrap';
    errorDisplay.style.wordBreak = 'break-word';
    errorDisplay.style.overflowWrap = 'anywhere';
    errorDisplay.style.maxWidth = '100%';

    errorDisplay.textContent = `${severity.toUpperCase()}: ${errorMsg}`;

    if (details && details.length > 0) {
        errorDisplay.textContent += '\n\n' + details.map(d => `  ${d}`).join('\n');
    }

    resultsContainer.insertBefore(errorDisplay, resultsContainer.firstChild);

    // Update glyph + title bar background to indicate error state
    const errorBg = severity === 'error' ? 'rgba(61, 31, 31, 0.92)' : 'rgba(61, 61, 31, 0.92)';
    const errorTitleBg = severity === 'error' ? '#3d1f1f' : '#3d3d1f';
    glyph.style.backgroundColor = errorBg;
    const errorTitleBar = glyph.querySelector('.canvas-glyph-title-bar') as HTMLElement;
    if (errorTitleBar) errorTitleBar.style.backgroundColor = errorTitleBg;

    log.debug(SEG.GLYPH, `[SeGlyph] Displayed ${severity} for ${glyphId}:`, errorMsg);
}
