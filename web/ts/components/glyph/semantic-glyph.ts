/**
 * Semantic Search Glyph (⊨) — natural language query on canvas
 *
 * Structurally identical to AX glyph: query input → watcher on backend → live results.
 * Difference: AX sends structured AX query syntax; SE sends natural language matched
 * by cosine similarity against attestation embeddings on the server.
 *
 * No local WASM query — semantic matching requires server-side embeddings.
 */

import type { Glyph } from '@qntx/glyphs';
import { SE } from '@generated/sym.js';
import { log, SEG } from '../../logger';
import { preventDrag, storeCleanup, setupGlyphResizeObserver } from '@qntx/glyphs';
import { canvasPlaced } from '@qntx/glyphs';
import { sendMessage } from '../../websocket';
import { apiFetch } from '../../api';
import type { Attestation } from '../../generated/proto/plugin/grpc/protocol/atsstore';
import { tooltip } from '../tooltip';
import { spawnAttestationGlyph } from './attestation-glyph';
import { isSigmaAttestation, renderSigmaResultLine } from './sigma-glyph';
import { uiState } from '../../state/ui';
import { syncStateManager } from '../../state/sync-state';
import { connectivityManager } from '../../connectivity';
import { el } from '../../html-utils';
import {
    createColorStateSetter,
    appendEmptyState,
    showQueryError,
} from './query-glyph-states';

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
    const symbol = el('span', {
        text: SE,
        style: { cursor: 'move', fontWeight: 'bold', flex: 'none', color: 'var(--glyph-status-running-text)' },
    });

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
    const editor = el('input', {
        class: 'se-query-input',
        style: {
            flex: '1', padding: '4px 8px', fontSize: '13px', fontFamily: 'monospace',
            border: 'none', outline: 'none', backgroundColor: 'rgba(25, 25, 30, 0.95)',
            color: '#d4f0d4', borderRadius: '2px',
        },
    });
    editor.type = 'text';
    editor.value = currentQuery;
    editor.placeholder = 'Semantic query (natural language)';

    preventDrag(editor);

    // Cluster scope dropdown
    const clusterSelect = el('select', {
        class: 'se-cluster-select',
        style: {
            padding: '2px 4px', fontSize: '11px', fontFamily: 'monospace',
            border: 'none', outline: 'none', backgroundColor: 'rgba(25, 25, 30, 0.95)',
            color: 'var(--text-secondary)', borderRadius: '2px', cursor: 'pointer',
            flexShrink: '0',
        },
    });

    const allOption = el('option', { text: 'All' });
    allOption.value = '';
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
                const notice = el('div', {
                    class: 'se-glyph-unavailable',
                    style: {
                        padding: '16px', color: 'var(--text-secondary)', fontSize: '12px',
                        fontFamily: 'monospace', textAlign: 'center', lineHeight: '1.6',
                    },
                });
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
                        const opt = el('option', {
                            text: c.label
                                ? `#${c.id} ${c.label} (${c.members})`
                                : `#${c.id} (${c.members})`,
                        });
                        opt.value = String(c.id);
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
                            const opt = el('option', { text: `#${id} (${count})` });
                            opt.value = id;
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
    const thresholdContainer = el('div', {
        style: { display: 'flex', alignItems: 'center', gap: '4px', flexShrink: '0' },
    });

    const thresholdSlider = el('input', {
        style: { width: '60px', cursor: 'pointer' },
    });
    thresholdSlider.type = 'range';
    thresholdSlider.min = '0.1';
    thresholdSlider.max = '1.0';
    thresholdSlider.step = '0.05';
    thresholdSlider.value = String(currentThreshold);

    preventDrag(thresholdSlider);

    const thresholdLabel = el('span', {
        text: currentThreshold.toFixed(2),
        style: { fontSize: '11px', color: 'var(--text-secondary)', fontFamily: 'monospace', minWidth: '28px' },
    });

    thresholdContainer.appendChild(thresholdSlider);
    thresholdContainer.appendChild(thresholdLabel);

    // Title bar (symbol + query input + threshold)
    const titleBar = el('div', {
        class: 'glyph-title-bar',
        style: { padding: '4px 4px 4px 8px' },
    });

    titleBar.appendChild(symbol);
    titleBar.appendChild(editor);
    titleBar.appendChild(clusterSelect);
    titleBar.appendChild(thresholdContainer);

    element.appendChild(titleBar);

    // Color states (shared palette with AX)
    const setColorState = createColorStateSetter(element, titleBar);
    setColorState('idle');

    // Results container
    const resultsContainer = el('div', {
        class: 'se-glyph-results glyph-content-area',
        style: {
            backgroundColor: 'rgba(25, 25, 30, 0.95)',
            borderTop: '1px solid var(--border)',
            fontSize: '12px', fontFamily: 'monospace',
        },
    });

    appendEmptyState(resultsContainer, 'se-glyph-empty-state');

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

        if (connectivityManager.state === 'online') {
            sendMessage({
                type: 'watcher_upsert',
                watcher_id: `se-glyph-${glyphId}`,
                semantic_query: query,
                semantic_threshold: currentThreshold,
                semantic_cluster_id: currentClusterId,
                watcher_name: `SE: ${query.substring(0, 30)}${query.length > 30 ? '...' : ''}`,
                enabled: true
            });
            setColorState('teal');
        } else {
            setColorState('orange');
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
        appendEmptyState(resultsContainer, 'se-glyph-empty-state');

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
    setupGlyphResizeObserver(element, resultsContainer, `SE ${glyphId}`);

    // Disable server-side watcher on cleanup (glyph deletion)
    storeCleanup(element, () => {
        if (connectivityManager.state === 'online') {
            sendMessage({
                type: 'watcher_upsert',
                watcher_id: `se-glyph-${glyphId}`,
                semantic_query: currentQuery,
                semantic_threshold: currentThreshold,
                semantic_cluster_id: currentClusterId,
                watcher_name: `SE: ${currentQuery.substring(0, 30)}`,
                enabled: false
            });
        }
    });

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
    const item = el('div', {
        class: 'se-glyph-result-item has-tooltip',
        style: {
            padding: '4px 8px', marginBottom: '2px',
            backgroundColor: 'rgba(31, 61, 31, 0.35)', borderRadius: '2px',
            cursor: 'pointer', display: 'flex', alignItems: 'center', gap: '8px',
        },
    });
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

    const text = el('div', {
        text: extractRichText(attestation),
        style: {
            fontSize: '11px', color: '#d4f0d4', fontFamily: 'monospace',
            wordBreak: 'break-word', overflowWrap: 'break-word', flex: '1',
        },
    });

    item.dataset.tooltip = buildAttestationTooltip(attestation);
    item.appendChild(text);

    // Score badge (right-aligned)
    if (score !== undefined && score > 0) {
        const badge = el('span', {
            text: `${Math.round(score * 100)}%`,
            style: { fontSize: '10px', fontFamily: 'monospace', color: 'var(--text-secondary)', flexShrink: '0' },
        });
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
        log.debug(SEG.GLYPH, `[SeGlyph] Cannot update results: results container not found for ${glyphId}`);
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

    const resultItem = isSigmaAttestation(attestation) ? renderSigmaResultLine(attestation) : renderAttestation(attestation, score);

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

    showQueryError(glyph, resultsContainer, 'se-glyph-empty-state', 'se-glyph-error', severity, errorMsg, 'SeGlyph', glyphId, details);
}
