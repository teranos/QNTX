/**
 * Test Glyphs - Demonstration of the glyph-primitive vision
 *
 * This file registers test glyphs to demonstrate the morphing behavior
 * where glyphs transform into windows and back.
 *
 * CURRENT STATE:
 * These are showcase glyphs demonstrating the core morphing mechanics.
 * The infrastructure is complete: single DOM element axiom, smooth animations,
 * proximity morphing, and window transformations all work.
 *
 * NEXT STEPS - The Migration Path:
 *
 * 1. GlyphSet - The Universal Container
 *    Instead of separate systems (windows, symbols, segments), we need a
 *    GlyphSet that holds all available glyphs. This becomes the single
 *    source of truth for all interactive visual elements in QNTX.
 *
 * 2. Symbol Palette → Glyph Migration
 *    The symbol palette (seg/sym system) should be the FIRST migration target.
 *    Each symbol becomes a glyph with:
 *    - Collapsed state: the symbol icon itself
 *    - Proximity state: symbol with label/description
 *    - Window state: full symbol details, relationships, attestations
 *
 * 3. Existing Windows → Glyph Migration
 *    Current windows (VidStream, Database, etc.) need to be converted to glyphs:
 *    - Add glyph registration with renderContent()
 *    - Remove old window creation code
 *    - Let the glyph system handle all morphing
 *
 * 4. Unified Interaction Model
 *    Once everything is a glyph:
 *    - Symbol palette becomes just another GlyphRun view
 *    - Windows are just expanded glyphs
 *    - Commands are glyphs that execute on click
 *    - Everything uses the same proximity/morphing behavior
 *
 * 5. Backend Alignment
 *    The glyph concept should eventually extend to the backend:
 *    - Attestations about glyphs
 *    - Glyph state persistence
 *    - Glyph relationships and dependencies
 *
 * The vision: Every visual element in QNTX is a glyph. They all morph
 * the same way, behave the same way, and are reasoned about the same way.
 * This creates conceptual clarity throughout the entire system.
 *
 * In the next session, we'll begin the real migration from seg/sym to glyphs,
 * starting with creating the GlyphSet infrastructure and converting the first
 * symbols into true glyphs.
 */

import * as d3 from 'd3';
import { glyphRun } from './components/glyph/run';
import { createCanvasGlyph } from './components/glyph/canvas/canvas-glyph';
import { createChartGlyph } from './components/glyph/chart-glyph';
import { sendMessage } from './websocket';
import { apiFetch } from './api';
import { escapeHtml } from './html-utils';
import { DB } from '@generated/sym.js';
import { log, SEG } from './logger.ts';
import { formatBuildTime, tooltip } from './components/tooltip.ts';
import type { VersionMessage, SystemCapabilitiesMessage, SyncStatusMessage } from '../types/websocket';
import { browserSync, type BrowserSyncState } from './browser-sync';
import { createPluginGlyph } from './plugin-panel.ts';

// Sync status state
let syncElement: HTMLElement | null = null;
let syncStatus: SyncStatusMessage | null = null;
let browserState: BrowserSyncState | null = null;

export function updateSyncStatus(data: SyncStatusMessage): void {
    syncStatus = data;
    if (syncElement) {
        renderSync();
    }
}

function renderSync(): void {
    if (!syncElement) return;

    if (!syncStatus) {
        syncElement.innerHTML = '<div class="glyph-loading">Loading sync status...</div>';
        return;
    }

    if (!syncStatus.available) {
        syncElement.innerHTML = `
            <div class="glyph-content">
                <div class="glyph-section">
                    <h3 class="glyph-section-title">Sync Engine</h3>
                    <div class="glyph-row">
                        <span class="glyph-label">Status:</span>
                        <span class="glyph-value" style="color: #fbbf24;">unavailable</span>
                    </div>
                    <div class="glyph-row">
                        <span class="glyph-label">Reason:</span>
                        <span class="glyph-value">${syncStatus.reason || 'unknown'}</span>
                    </div>
                </div>
                <div class="glyph-section">
                    <h3 class="glyph-section-title">What's Next</h3>
                    <div style="color: #9ca3af; font-size: 12px; line-height: 1.5; padding: 4px 0;">
                        Build with <code style="background: #1e293b; padding: 2px 6px; border-radius: 3px;">make wasm</code> to enable the sync engine.
                        The Merkle tree, content hashing, and symmetric reconciliation protocol are ready —
                        they need the qntx-core WASM module to run.
                    </div>
                </div>
            </div>
        `;
        return;
    }

    if (syncStatus.error) {
        syncElement.innerHTML = `
            <div class="glyph-content">
                <div class="glyph-row">
                    <span class="glyph-label">Status:</span>
                    <span class="glyph-value" style="color: #f87171;">error</span>
                </div>
                <div class="glyph-row">
                    <span class="glyph-label">Error:</span>
                    <span class="glyph-value">${syncStatus.error}</span>
                </div>
            </div>
        `;
        return;
    }

    const rootShort = syncStatus.root
        ? `${syncStatus.root.substring(0, 8)}...${syncStatus.root.substring(56)}`
        : 'empty';
    const rootColor = syncStatus.root && syncStatus.root !== '0'.repeat(64) ? '#4ade80' : '#9ca3af';

    // Browser convergence
    const browserRoot = browserState?.root || '';
    const browserRootShort = browserRoot
        ? `${browserRoot.substring(0, 8)}...${browserRoot.substring(56)}`
        : 'not initialized';
    const converged = browserRoot && syncStatus.root && browserRoot === syncStatus.root;
    const convergenceLabel = !browserRoot ? 'initializing'
        : browserState?.syncing ? `syncing (round ${browserState.round})`
        : converged ? 'converged' : 'divergent';
    const convergenceColor = !browserRoot ? '#9ca3af'
        : browserState?.syncing ? '#fbbf24'
        : converged ? '#4ade80' : '#f87171';

    // Peers section
    const peers = syncStatus.peers || [];
    let peersSection = '';
    if (peers.length > 0) {
        const peerRows = peers.map(p => {
            const isSelf = p.status === 'self';
            const statusColor = p.status === 'ok' ? '#4ade80' : p.status === 'unreachable' ? '#f87171' : '#6b7280';
            const statusDot = p.status ? `<span style="color: ${statusColor}; margin-right: 6px;">●</span>` : '';
            const syncBtn = isSelf ? '' : `<button class="sync-peer-btn" data-peer-url="${p.url}" style="
                    background: transparent; border: 1px solid #60a5fa; color: #60a5fa;
                    padding: 2px 8px; border-radius: 3px; cursor: pointer;
                    font-family: monospace; font-size: 11px; margin-left: 8px;
                ">Sync</button>`;
            const advertisedName = p.advertised_name ? ` <span style="color: #9ca3af;">(${escapeHtml(p.advertised_name)})</span>` : '';
            return `
            <div class="glyph-row" style="align-items: center;">
                ${statusDot}<span class="glyph-label">${escapeHtml(p.name)}${advertisedName}:</span>
                <span class="glyph-value" style="font-size: 11px; flex: 1;">${escapeHtml(p.url)}</span>
                ${syncBtn}
            </div>`;
        }).join('');
        peersSection = `
            <div class="glyph-section">
                <h3 class="glyph-section-title">Configured Peers</h3>
                ${peerRows}
            </div>
        `;
    } else {
        peersSection = `
            <div class="glyph-section">
                <h3 class="glyph-section-title">Peers</h3>
                <div style="color: #9ca3af; font-size: 12px; line-height: 1.5; padding: 4px 0;">
                    No peers configured. Add peers to <code style="background: #1e293b; padding: 2px 6px; border-radius: 3px;">am.toml</code>:
                    <pre style="margin: 8px 0 0; background: #0f172a; padding: 8px; border-radius: 4px; font-size: 11px; overflow-x: auto;">[sync.peers]
phone = "http://phone.local:877"</pre>
                </div>
            </div>
        `;
    }

    // Vision teaser
    const visionSection = `
        <div class="glyph-section">
            <h3 class="glyph-section-title">The Road Ahead</h3>
            <div style="color: #9ca3af; font-size: 12px; line-height: 1.6; padding: 4px 0;">
                <div style="margin-bottom: 6px;"><span style="color: #60a5fa;">Reactive push</span> — new attestations trigger immediate peer sync</div>
                <div style="margin-bottom: 6px;"><span style="color: #60a5fa;">Authentication</span> — peer identity verification before reconciliation</div>
                <div><span style="color: #60a5fa;">Reticulum</span> — cryptographic mesh transport beneath the same protocol</div>
            </div>
        </div>
    `;

    syncElement.innerHTML = `
        <div class="glyph-content">
            <div class="glyph-section">
                <h3 class="glyph-section-title">Merkle Tree</h3>
                <div class="glyph-row">
                    <span class="glyph-label">Server:</span>
                    <span class="glyph-value" style="color: ${rootColor}; font-family: monospace; font-size: 12px;">${rootShort}</span>
                </div>
                <div class="glyph-row">
                    <span class="glyph-label">Browser:</span>
                    <span class="glyph-value" style="color: ${browserRoot ? rootColor : '#9ca3af'}; font-family: monospace; font-size: 12px;">${browserRootShort}</span>
                </div>
                <div class="glyph-row" style="align-items: center;">
                    <span class="glyph-label">Status:</span>
                    <span class="glyph-value" style="color: ${convergenceColor}; flex: 1;"><span style="margin-right: 4px;">${converged ? '\u25CF' : browserState?.syncing ? '\u25CB' : '\u25CF'}</span>${convergenceLabel}</span>
                    <button class="browser-sync-btn" style="
                        background: transparent; border: 1px solid #60a5fa; color: #60a5fa;
                        padding: 2px 8px; border-radius: 3px; cursor: pointer;
                        font-family: monospace; font-size: 11px; margin-left: 8px;
                    " ${browserState?.syncing ? 'disabled' : ''}>Sync</button>
                </div>
                <div class="glyph-row">
                    <span class="glyph-label">Groups:</span>
                    <span class="glyph-value">${(syncStatus.groups ?? 0).toLocaleString()} <span style="color: #6b7280;">server</span> / ${(browserState?.groups ?? 0).toLocaleString()} <span style="color: #6b7280;">browser</span></span>
                </div>
            </div>
            ${peersSection}
            ${visionSection}
        </div>
    `;

    // Browser sync button — triggers manual reconciliation
    const browserSyncBtn = syncElement.querySelector('.browser-sync-btn');
    if (browserSyncBtn) {
        browserSyncBtn.addEventListener('click', async () => {
            const button = browserSyncBtn as HTMLButtonElement;
            button.textContent = 'Syncing\u2026';
            button.disabled = true;
            button.style.borderColor = '#fbbf24';
            button.style.color = '#fbbf24';

            try {
                const { sent, received } = await browserSync.reconcile();
                button.textContent = `\u2191${sent} \u2193${received}`;
                button.style.borderColor = '#4ade80';
                button.style.color = '#4ade80';
            } catch {
                button.textContent = 'Failed';
                button.style.borderColor = '#f87171';
                button.style.color = '#f87171';
            }

            setTimeout(() => {
                button.textContent = 'Sync';
                button.disabled = false;
                button.style.borderColor = '#60a5fa';
                button.style.color = '#60a5fa';
            }, 3000);
        });
    }

    // Attach per-peer sync button handlers
    syncElement.querySelectorAll('.sync-peer-btn').forEach(btn => {
        btn.addEventListener('click', async () => {
            const button = btn as HTMLButtonElement;
            const peerUrl = button.dataset.peerUrl!;

            button.textContent = 'Syncing\u2026';
            button.disabled = true;
            button.style.borderColor = '#fbbf24';
            button.style.color = '#fbbf24';

            try {
                const resp = await apiFetch('/api/sync', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ peer: peerUrl }),
                });
                const data = await resp.json();

                if (data.error) {
                    button.textContent = 'Error';
                    button.title = data.error;
                    button.style.borderColor = '#f87171';
                    button.style.color = '#f87171';
                } else {
                    button.textContent = `\u2191${data.sent} \u2193${data.received}`;
                    button.style.borderColor = '#4ade80';
                    button.style.color = '#4ade80';
                    sendMessage({ type: 'get_sync_status' });
                }
            } catch {
                button.textContent = 'Failed';
                button.style.borderColor = '#f87171';
                button.style.color = '#f87171';
            }

            setTimeout(() => {
                button.textContent = 'Sync';
                button.disabled = false;
                button.style.borderColor = '#60a5fa';
                button.style.color = '#60a5fa';
                button.title = '';
            }, 3000);
        });
    });
}

// Database stats state
let dbStatsElement: HTMLElement | null = null;
let dbStats: any = null;

// Embeddings state
let embeddingsElement: HTMLElement | null = null;
let embeddingsInfo: {
    available: boolean;
    model_name: string;
    dimensions: number;
    embedding_count: number;
    attestation_count: number;
    unembedded_ids?: string[];
    cluster_info?: { n_clusters: number; n_noise: number; n_total: number; clusters: Record<string, number> };
    hdbscan_config?: { min_cluster_size: number; cluster_threshold: number; cluster_match_threshold: number };
} | null = null;
let embeddingsReembedding = false;
let embeddingsClustering = false;
let embeddingsProjecting = false;
type ProjectionPoint = { id: string; source_id: string; method: string; x: number; y: number; cluster_id: number };
let projectionsData: Record<string, ProjectionPoint[]> = {};
let clusterLabels: Map<number, string | null> = new Map();
const clusterSamplesCache: Map<number, string[]> = new Map();
type TimelinePoint = { run_id: string; run_time: string; n_points: number; n_noise: number; cluster_id: number; label: string | null; n_members: number; event_type: string };
let timelineData: TimelinePoint[] = [];

// Self diagnostics state
let selfElement: HTMLElement | null = null;
let selfVersion: VersionMessage | null = null;
let selfCapabilities: SystemCapabilitiesMessage | null = null;

export function updateDatabaseStats(stats: any): void {
    dbStats = stats;
    if (dbStatsElement) {
        renderDbStats();
    }
}

export function updateSelfVersion(data: VersionMessage): void {
    selfVersion = data;
    if (selfElement) {
        renderSelf();
    }
}

export function updateSelfCapabilities(data: SystemCapabilitiesMessage): void {
    selfCapabilities = data;
    if (selfElement) {
        renderSelf();
    }
}

function renderDbStats(): void {
    if (!dbStatsElement) return;

    if (!dbStats) {
        dbStatsElement.innerHTML = '<div class="glyph-loading">Loading database statistics...</div>';
        return;
    }

    const storageBackend = dbStats.storage_optimized
        ? `rust (optimized) v${dbStats.storage_version}`
        : 'go (fallback)';

    dbStatsElement.innerHTML = `
        <div class="glyph-content">
            <div class="glyph-row">
                <span class="glyph-label">Database Path:</span>
                <span class="glyph-value">${dbStats.path}</span>
            </div>
            <div class="glyph-row">
                <span class="glyph-label">Storage Backend:</span>
                <span class="glyph-value">${storageBackend}</span>
            </div>
            <div class="glyph-row">
                <span class="glyph-label">Total Attestations:</span>
                <span class="glyph-value">${dbStats.total_attestations.toLocaleString()}</span>
            </div>
            <div class="glyph-row">
                <span class="glyph-label">Unique Actors:</span>
                <span class="glyph-value">${dbStats.unique_actors.toLocaleString()}</span>
            </div>
            <div class="glyph-row">
                <span class="glyph-label">Unique Subjects:</span>
                <span class="glyph-value">${dbStats.unique_subjects.toLocaleString()}</span>
            </div>
            <div class="glyph-row">
                <span class="glyph-label">Unique Contexts:</span>
                <span class="glyph-value">${dbStats.unique_contexts.toLocaleString()}</span>
            </div>
        </div>
    `;
}

function renderSelf(): void {
    if (!selfElement) return;

    if (!selfVersion && !selfCapabilities) {
        selfElement.innerHTML = '<div class="glyph-loading">Waiting for system info...</div>';
        return;
    }

    const sections: string[] = [];

    // QNTX Server version section
    if (selfVersion) {
        const buildTimeFormatted = formatBuildTime(selfVersion.build_time) || selfVersion.build_time || 'unknown';
        const commitShort = selfVersion.commit?.substring(0, 7) || 'unknown';

        sections.push(`
            <div class="glyph-section">
                <h3 class="glyph-section-title">QNTX Server</h3>
                <div class="glyph-row">
                    <span class="glyph-label">Version:</span>
                    <span class="glyph-value">${selfVersion.version || 'unknown'}</span>
                </div>
                <div class="glyph-row">
                    <span class="glyph-label">Commit:</span>
                    <span class="glyph-value">${commitShort}</span>
                </div>
                <div class="glyph-row">
                    <span class="glyph-label">Built:</span>
                    <span class="glyph-value">${buildTimeFormatted}</span>
                </div>
                ${selfVersion.go_version ? `
                <div class="glyph-row">
                    <span class="glyph-label">Go:</span>
                    <span class="glyph-value">${selfVersion.go_version}</span>
                </div>
                ` : ''}
            </div>
        `);
    }

    // System Capabilities section
    if (selfCapabilities) {
        const caps = selfCapabilities;

        const parserStatus = caps.parser_optimized ?
            `<span style="color: #4ade80;">✓ qntx-core WASM ${caps.parser_size ? `(${caps.parser_size})` : ''}</span>` :
            `<span style="color: #fbbf24;">⚠ Go native parser</span>`;

        const fuzzyBackendLabel = caps.fuzzy_backend === 'wasm' ? 'WASM' : caps.fuzzy_backend === 'rust' ? 'Rust' : 'Go';
        const fuzzyStatus = caps.fuzzy_optimized ?
            `<span style="color: #4ade80;">✓ Optimized (${fuzzyBackendLabel})</span>` :
            `<span style="color: #fbbf24;">⚠ Fallback (Go)</span>`;

        const vidstreamStatus = caps.vidstream_optimized ?
            `<span style="color: #4ade80;">✓ Available (ONNX)</span>` :
            `<span style="color: #fbbf24;">⚠ Unavailable</span>`;

        const storageStatus = caps.storage_optimized ?
            `<span style="color: #4ade80;">✓ Optimized (Rust)</span>` :
            `<span style="color: #fbbf24;">⚠ Fallback (Go)</span>`;

        sections.push(`
            <div class="glyph-section">
                <h3 class="glyph-section-title">System Capabilities</h3>
                <div class="glyph-row">
                    <span class="glyph-label">parser:</span>
                    <span class="glyph-value">
                        ${caps.parser_version ? `v${caps.parser_version}` : ''}
                        ${parserStatus}
                    </span>
                </div>
                <div class="glyph-row">
                    <span class="glyph-label">fuzzy-ax:</span>
                    <span class="glyph-value">
                        ${caps.fuzzy_version ? `v${caps.fuzzy_version}` : 'unknown'}
                        ${fuzzyStatus}
                    </span>
                </div>
                <div class="glyph-row">
                    <span class="glyph-label">vidstream:</span>
                    <span class="glyph-value">
                        ${caps.vidstream_version ? `v${caps.vidstream_version}` : 'unknown'}
                        ${vidstreamStatus}
                    </span>
                </div>
                <div class="glyph-row">
                    <span class="glyph-label">storage:</span>
                    <span class="glyph-value">
                        ${caps.storage_version ? `v${caps.storage_version}` : 'unknown'}
                        ${storageStatus}
                    </span>
                </div>
            </div>
        `);
    }

    selfElement.innerHTML = `
        <div class="glyph-content">
            ${sections.join('\n')}
        </div>
    `;
}

export async function fetchEmbeddingsInfo(): Promise<void> {
    try {
        const [infoResp, projResp, clustersResp, timelineResp] = await Promise.all([
            apiFetch('/api/embeddings/info'),
            apiFetch('/api/embeddings/projections'),
            apiFetch('/api/embeddings/clusters'),
            apiFetch('/api/embeddings/cluster-timeline'),
        ]);
        embeddingsInfo = await infoResp.json();
        const raw = projResp.ok ? await projResp.json() : {};
        // Backend returns Record<string, ProjectionPoint[]> — validate shape
        if (raw && typeof raw === 'object' && !Array.isArray(raw)) {
            projectionsData = raw as Record<string, ProjectionPoint[]>;
        } else {
            projectionsData = {};
        }
        // Build cluster label map from /api/embeddings/clusters
        clusterLabels = new Map();
        if (clustersResp.ok) {
            const clusters = await clustersResp.json() as Array<{ id: number; label: string | null }>;
            for (const c of clusters) {
                clusterLabels.set(c.id, c.label);
            }
        }
        // Timeline data for cluster evolution chart
        timelineData = timelineResp.ok ? await timelineResp.json() as TimelinePoint[] : [];
    } catch {
        embeddingsInfo = null;
        projectionsData = {};
        clusterLabels = new Map();
        timelineData = [];
    }
    renderEmbeddings();
}

function renderEmbeddings(): void {
    if (!embeddingsElement) return;

    if (!embeddingsInfo) {
        embeddingsElement.innerHTML = '<div class="glyph-loading">Loading...</div>';
        return;
    }

    const { available, model_name, dimensions, embedding_count, attestation_count } = embeddingsInfo;
    const unembedded = attestation_count - embedding_count;

    let reembedSection = '';
    if (available && unembedded > 0) {
        reembedSection = `
            <div class="glyph-section" style="margin-top:8px;padding-top:8px;border-top:1px solid var(--border-color, #333)">
                <button class="emb-reembed-btn panel-btn" style="width:100%"
                    ${embeddingsReembedding ? 'disabled' : ''}>
                    ${embeddingsReembedding ? 'Embedding...' : `Embed ${unembedded} unembedded attestations`}
                </button>
                <div class="emb-result" style="margin-top:6px;font-size:12px;opacity:0.7"></div>
            </div>
        `;
    }

    // Cluster info section
    const ci = embeddingsInfo.cluster_info;
    let clusterSection = '';
    if (available && embedding_count >= 2) {
        let clusterRows = '';
        if (ci && ci.n_clusters > 0) {
            const pillColor = d3.scaleOrdinal(d3.schemeTableau10);
            const clusterPills = Object.entries(ci.clusters)
                .sort(([a], [b]) => Number(a) - Number(b))
                .map(([id, count]) => {
                    const c = pillColor(id);
                    const label = clusterLabels.get(Number(id));
                    const labelText = label ? ` ${escapeHtml(label)}` : '';
                    const tooltipDefault = label ? `#${id} ${escapeHtml(label)}` : `#${id}`;
                    return `<span class="emb-cluster-pill has-tooltip" data-cluster-id="${id}" data-tooltip="${escapeHtml(tooltipDefault)}" style="display:inline-flex;align-items:center;gap:4px;padding:2px 8px;border-radius:10px;background:${c}22;border:1px solid ${c}55;cursor:pointer;white-space:nowrap;font-size:11px;line-height:1.4"><span style="color:${c};font-weight:bold">#${id}</span>${labelText ? `<span style="color:#a0aec0">${labelText}</span>` : ''}<span style="color:#9ca3af">:${count}</span></span>`;
                })
                .join('');
            clusterRows = `
                <div class="glyph-row">
                    <span class="glyph-label">Clusters:</span>
                    <span class="glyph-value">${ci.n_clusters}</span>
                </div>
                <div class="glyph-row">
                    <span class="glyph-label">Noise:</span>
                    <span class="glyph-value">${ci.n_noise}</span>
                </div>
                <div style="display:flex;flex-wrap:wrap;gap:4px;margin-top:4px">${clusterPills}</div>
            `;
        } else {
            clusterRows = `
                <div class="glyph-row">
                    <span class="glyph-label">Clusters:</span>
                    <span class="glyph-value" style="color:#6b7280">not computed</span>
                </div>
            `;
        }
        const hc = embeddingsInfo.hdbscan_config;
        const minCS = hc?.min_cluster_size ?? 5;
        const ct = hc?.cluster_threshold ?? 0.5;
        const cmt = hc?.cluster_match_threshold ?? 0.7;
        clusterSection = `
            <div class="glyph-section" style="margin-top:8px;padding-top:8px;border-top:1px solid var(--border-color, #333)">
                <h3 class="glyph-section-title">HDBSCAN Clustering</h3>
                ${clusterRows}
                <div style="display:flex;flex-wrap:wrap;gap:6px;margin-top:6px;font-size:11px;align-items:center">
                    <label style="color:#9ca3af;display:inline-flex;align-items:center;gap:3px">size<input class="emb-param emb-param-min-cluster-size" type="number" min="2" max="50" step="1" value="${minCS}" style="width:36px;padding:1px 3px;background:var(--input-bg, #1a1a2e);border:1px solid var(--border-color, #333);color:var(--text-color, #e0e0e0);border-radius:3px;font-size:11px;text-align:right"></label>
                    <label style="color:#9ca3af;display:inline-flex;align-items:center;gap:3px">thresh<input class="emb-param emb-param-cluster-threshold" type="number" min="0.1" max="1.0" step="0.05" value="${ct}" style="width:42px;padding:1px 3px;background:var(--input-bg, #1a1a2e);border:1px solid var(--border-color, #333);color:var(--text-color, #e0e0e0);border-radius:3px;font-size:11px;text-align:right"></label>
                    <label style="color:#9ca3af;display:inline-flex;align-items:center;gap:3px">match<input class="emb-param emb-param-match-threshold" type="number" min="0.1" max="1.0" step="0.05" value="${cmt}" style="width:42px;padding:1px 3px;background:var(--input-bg, #1a1a2e);border:1px solid var(--border-color, #333);color:var(--text-color, #e0e0e0);border-radius:3px;font-size:11px;text-align:right"></label>
                    <button class="emb-cluster-btn panel-btn" style="margin-left:auto;padding:2px 10px;font-size:11px"
                        ${embeddingsClustering ? 'disabled' : ''}>
                        ${embeddingsClustering ? 'Clustering...' : 'Recompute'}
                    </button>
                </div>
                <div class="emb-cluster-result" style="margin-top:6px;font-size:12px;opacity:0.7"></div>
            </div>
        `;
    }

    // Scatter section: side-by-side projections or project button
    const methodNames = Object.keys(projectionsData);
    const hasProjections = methodNames.some(m => projectionsData[m]?.length > 0);
    let scatterSection = '';
    if (available && embedding_count >= 2) {
        if (hasProjections) {
            const inputStyle = 'padding:2px 4px;background:var(--input-bg, #1a1a2e);border:1px solid var(--border-color, #333);color:var(--text-color, #e0e0e0);border-radius:3px;font-size:11px;text-align:right;-moz-appearance:textfield';
            const methodParams: Record<string, string> = {
                umap: `<label style="color:#9ca3af;display:inline-flex;align-items:center;gap:3px">neighbors<input class="emb-param emb-param-n-neighbors" type="number" min="2" max="200" step="1" value="15" style="width:40px;${inputStyle}"></label> <label style="color:#9ca3af;display:inline-flex;align-items:center;gap:3px">min_dist<input class="emb-param emb-param-min-dist" type="number" min="0.0" max="1.0" step="0.05" value="0.1" style="width:42px;${inputStyle}"></label>`,
                tsne: `<label style="color:#9ca3af;display:inline-flex;align-items:center;gap:3px">perplexity<input class="emb-param emb-param-perplexity" type="number" min="5" max="100" step="5" value="30" style="width:40px;${inputStyle}"></label>`,
            };
            const scatterSlots = methodNames
                .filter(m => projectionsData[m]?.length > 0)
                .map(m => {
                    const pts = projectionsData[m];
                    const nClusters = new Set(pts.filter(p => p.cluster_id !== -1).map(p => p.cluster_id)).size;
                    const params = methodParams[m] || '';
                    return `<div style="flex:1;min-width:0">
                        <div style="font-size:11px;color:#9ca3af;text-align:center;margin-bottom:4px">${m.toUpperCase()} (${pts.length}pts, ${nClusters}cl)</div>
                        <div class="emb-scatter" data-method="${m}"></div>
                        ${params ? `<div style="display:flex;flex-wrap:wrap;gap:4px;margin-top:4px;font-size:11px;justify-content:center">${params}</div>` : ''}
                    </div>`;
                }).join('');
            scatterSection = `
                <div class="glyph-section" style="margin-top:8px;padding-top:8px;border-top:1px solid var(--border-color, #333)">
                    <h3 class="glyph-section-title">Projections</h3>
                    <div style="display:flex;gap:6px">${scatterSlots}</div>
                    <div style="display:flex;justify-content:flex-end;margin-top:6px">
                        <button class="emb-project-btn panel-btn" style="padding:2px 10px;font-size:11px"
                            ${embeddingsProjecting ? 'disabled' : ''}>
                            ${embeddingsProjecting ? 'Projecting...' : 'Re-project'}
                        </button>
                    </div>
                    <div class="emb-project-result" style="margin-top:6px;font-size:12px;opacity:0.7"></div>
                </div>
            `;
        } else {
            scatterSection = `
                <div class="glyph-section" style="margin-top:8px;padding-top:8px;border-top:1px solid var(--border-color, #333)">
                    <h3 class="glyph-section-title">Projections</h3>
                    <div class="glyph-row">
                        <span class="glyph-label">Status:</span>
                        <span class="glyph-value" style="color:#6b7280">not computed</span>
                    </div>
                    <div style="display:flex;justify-content:flex-end;margin-top:6px">
                        <button class="emb-project-btn panel-btn" style="padding:2px 10px;font-size:11px"
                            ${embeddingsProjecting ? 'disabled' : ''}>
                            ${embeddingsProjecting ? 'Projecting...' : 'Project'}
                        </button>
                    </div>
                    <div class="emb-project-result" style="margin-top:6px;font-size:12px;opacity:0.7"></div>
                </div>
            `;
        }
    }

    // Timeline section: stacked area chart showing cluster evolution across runs
    const runIDs = new Set(timelineData.map(p => p.run_id));
    let timelineSection = '';
    if (available && runIDs.size >= 2) {
        timelineSection = `
            <div class="glyph-section" style="margin-top:8px;padding-top:8px;border-top:1px solid var(--border-color, #333)">
                <h3 class="glyph-section-title">Cluster Timeline</h3>
                <div class="emb-timeline"></div>
            </div>
        `;
    } else if (available && runIDs.size === 1) {
        timelineSection = `
            <div class="glyph-section" style="margin-top:8px;padding-top:8px;border-top:1px solid var(--border-color, #333)">
                <h3 class="glyph-section-title">Cluster Timeline</h3>
                <div style="font-size:12px;color:#6b7280">Need 2+ clustering runs for timeline</div>
            </div>
        `;
    }

    embeddingsElement.innerHTML = `
        <style>.emb-param::-webkit-inner-spin-button,.emb-param::-webkit-outer-spin-button{-webkit-appearance:none;margin:0}.emb-param{-moz-appearance:textfield}</style>
        <div class="glyph-content">
            <div class="glyph-row">
                <span class="glyph-label">Status:</span>
                <span class="glyph-value">${available ? '<span style="color:#4ade80">Active</span>' : '<span style="color:#fbbf24">Unavailable</span>'}</span>
            </div>
            ${available ? `
            <div class="glyph-row">
                <span class="glyph-label">Model:</span>
                <span class="glyph-value">${model_name}</span>
            </div>
            <div class="glyph-row">
                <span class="glyph-label">Dimensions:</span>
                <span class="glyph-value">${dimensions}</span>
            </div>
            ` : ''}
            <div class="glyph-row">
                <span class="glyph-label">Embedded:</span>
                <span class="glyph-value">${embedding_count} / ${attestation_count}</span>
            </div>
            ${reembedSection}
            ${clusterSection}
            ${scatterSection}
            ${timelineSection}
        </div>
    `;

    const btn = embeddingsElement.querySelector('.emb-reembed-btn');
    if (btn) {
        btn.addEventListener('click', reembedAll);
    }

    const clusterBtn = embeddingsElement.querySelector('.emb-cluster-btn');
    if (clusterBtn) {
        clusterBtn.addEventListener('click', recluster);
    }

    const projectBtn = embeddingsElement.querySelector('.emb-project-btn');
    if (projectBtn) {
        projectBtn.addEventListener('click', projectAll);
    }

    // Cluster pill hover tooltips — lazy-fetch sample texts on first hover
    tooltip.attach(embeddingsElement, '.emb-cluster-pill');
    embeddingsElement.querySelectorAll('.emb-cluster-pill').forEach(pill => {
        const el = pill as HTMLElement;
        const cid = Number(el.dataset.clusterId);
        el.addEventListener('mouseenter', async () => {
            if (clusterSamplesCache.has(cid)) return;
            try {
                const resp = await apiFetch(`/api/embeddings/clusters/samples?cluster_id=${cid}&size=5`);
                if (!resp.ok) return;
                const data = await resp.json();
                const samples = data.samples as string[];
                clusterSamplesCache.set(cid, samples);
                const label = clusterLabels.get(cid);
                const header = label ? `#${cid} ${label}` : `#${cid}`;
                el.dataset.tooltip = header + '\n' + samples.map((s, i) => `${i + 1}. ${s}`).join('\n');
            } catch { /* ignore */ }
        }, { once: true });

        // Click → drill-down into cluster detail view
        el.addEventListener('click', () => {
            renderClusterDetail(cid);
        });
    });

    embeddingsElement.querySelectorAll('.emb-scatter[data-method]').forEach(el => {
        const container = el as HTMLElement;
        const method = container.dataset.method!;
        const data = projectionsData[method];
        if (data?.length > 0) {
            renderScatter(container, data);
        }
    });

    const timelineContainer = embeddingsElement.querySelector('.emb-timeline') as HTMLElement | null;
    if (timelineContainer && timelineData.length > 0) {
        renderTimeline(timelineContainer, timelineData);
    }
}

// Track active keydown handler so we can clean it up on back/navigate
let clusterDetailKeyHandler: ((e: KeyboardEvent) => void) | null = null;

function getClusterIDs(): number[] {
    const ci = embeddingsInfo?.cluster_info;
    if (!ci?.clusters) return [];
    return Object.keys(ci.clusters).map(Number).sort((a, b) => a - b);
}

async function renderClusterDetail(clusterID: number): Promise<void> {
    if (!embeddingsElement) return;

    // Clean up previous keydown handler
    if (clusterDetailKeyHandler) {
        document.removeEventListener('keydown', clusterDetailKeyHandler);
        clusterDetailKeyHandler = null;
    }

    const label = clusterLabels.get(clusterID);
    const ci = embeddingsInfo?.cluster_info;
    const memberCount = ci?.clusters?.[String(clusterID)] ?? 0;
    const pillColor = d3.scaleOrdinal(d3.schemeTableau10);
    const color = pillColor(String(clusterID));
    const title = label ? `#${clusterID} ${escapeHtml(label)}` : `#${clusterID}`;

    const clusterIDs = getClusterIDs();
    const idx = clusterIDs.indexOf(clusterID);
    const prevID = idx > 0 ? clusterIDs[idx - 1] : null;
    const nextID = idx < clusterIDs.length - 1 ? clusterIDs[idx + 1] : null;

    embeddingsElement.innerHTML = `
        <div class="glyph-content">
            <div style="display:flex;align-items:center;gap:8px;margin-bottom:8px">
                <button class="emb-back-btn panel-btn" style="padding:2px 10px;font-size:12px">← Back</button>
                <button class="emb-prev-btn panel-btn" style="padding:2px 8px;font-size:12px" ${prevID === null ? 'disabled' : ''}>◀</button>
                <span style="font-size:14px;font-weight:bold;color:${color}">${title}</span>
                <span style="color:#9ca3af;font-size:12px">${memberCount} members</span>
                <button class="emb-next-btn panel-btn" style="padding:2px 8px;font-size:12px" ${nextID === null ? 'disabled' : ''}>▶</button>
                <span style="color:#6b7280;font-size:10px;margin-left:auto">${idx + 1}/${clusterIDs.length} ← →</span>
            </div>
            <div class="glyph-section" style="border-top:1px solid var(--border-color, #333);padding-top:8px">
                <h3 class="glyph-section-title">Projection</h3>
                <div style="display:flex;gap:6px" class="emb-detail-scatters"></div>
            </div>
            <div class="glyph-section" style="margin-top:8px;padding-top:8px;border-top:1px solid var(--border-color, #333)">
                <h3 class="glyph-section-title">Cluster History</h3>
                <div class="emb-detail-timeline"></div>
            </div>
            <div class="glyph-section" style="margin-top:8px;padding-top:8px;border-top:1px solid var(--border-color, #333)">
                <h3 class="glyph-section-title">Recent Attestations</h3>
                <div class="emb-detail-members" style="font-size:12px;color:#9ca3af">Loading...</div>
            </div>
        </div>
    `;

    // Back button
    const cleanupAndBack = () => {
        if (clusterDetailKeyHandler) {
            document.removeEventListener('keydown', clusterDetailKeyHandler);
            clusterDetailKeyHandler = null;
        }
        renderEmbeddings();
    };
    embeddingsElement.querySelector('.emb-back-btn')?.addEventListener('click', cleanupAndBack);

    // Prev/Next buttons
    if (prevID !== null) {
        embeddingsElement.querySelector('.emb-prev-btn')?.addEventListener('click', () => renderClusterDetail(prevID));
    }
    if (nextID !== null) {
        embeddingsElement.querySelector('.emb-next-btn')?.addEventListener('click', () => renderClusterDetail(nextID));
    }

    // Arrow key navigation
    clusterDetailKeyHandler = (e: KeyboardEvent) => {
        if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement) return;
        if (e.key === 'ArrowLeft' && prevID !== null) {
            e.preventDefault();
            renderClusterDetail(prevID);
        } else if (e.key === 'ArrowRight' && nextID !== null) {
            e.preventDefault();
            renderClusterDetail(nextID);
        } else if (e.key === 'Escape') {
            e.preventDefault();
            cleanupAndBack();
        }
    };
    document.addEventListener('keydown', clusterDetailKeyHandler);

    // Projection scatter — highlight this cluster, dim rest
    const scatterContainer = embeddingsElement.querySelector('.emb-detail-scatters') as HTMLElement;
    if (scatterContainer) {
        for (const method of Object.keys(projectionsData)) {
            const pts = projectionsData[method];
            if (!pts || pts.length === 0) continue;
            const slot = document.createElement('div');
            slot.style.flex = '1';
            slot.style.minWidth = '0';
            const header = document.createElement('div');
            header.style.fontSize = '11px';
            header.style.color = '#9ca3af';
            header.style.textAlign = 'center';
            header.style.marginBottom = '4px';
            header.textContent = method.toUpperCase();
            slot.appendChild(header);
            const canvas = document.createElement('div');
            slot.appendChild(canvas);
            scatterContainer.appendChild(slot);
            renderScatterHighlighted(canvas, pts, clusterID);
        }
    }

    // Fetch member attestations
    const membersContainer = embeddingsElement.querySelector('.emb-detail-members') as HTMLElement;
    try {
        const resp = await apiFetch(`/api/embeddings/clusters/members?cluster_id=${clusterID}&limit=20`);
        if (resp.ok) {
            const data = await resp.json();
            const attestations = data.attestations as any[];
            if (attestations.length === 0) {
                membersContainer.textContent = 'No attestations found';
            } else {
                membersContainer.innerHTML = '';
                for (const as of attestations) {
                    const row = document.createElement('div');
                    row.className = 'has-tooltip';
                    row.style.padding = '4px 8px';
                    row.style.marginBottom = '2px';
                    row.style.backgroundColor = 'rgba(31, 61, 31, 0.35)';
                    row.style.borderRadius = '2px';
                    row.style.cursor = 'pointer';
                    row.style.fontSize = '11px';
                    row.style.fontFamily = 'monospace';
                    row.style.wordBreak = 'break-word';
                    row.style.overflowWrap = 'break-word';

                    const subjects = as.subjects?.join(', ') || '?';
                    const predicates = as.predicates?.join(', ') || '?';
                    const contexts = as.contexts?.join(', ') || '?';
                    row.innerHTML = `<span style="color:#60a5fa">${escapeHtml(subjects)}</span> <span style="color:#9ca3af">is</span> <span style="color:#4ade80">${escapeHtml(predicates)}</span> <span style="color:#9ca3af">of</span> <span style="color:#c084fc">${escapeHtml(contexts)}</span>`;

                    // Build tooltip from attributes
                    const tipLines: string[] = [];
                    if (as.attributes && typeof as.attributes === 'object') {
                        for (const [key, value] of Object.entries(as.attributes)) {
                            if (key === 'rich_string_fields') continue;
                            const display = typeof value === 'string' ? value : JSON.stringify(value);
                            const truncated = display.length > 120 ? display.substring(0, 120) + '...' : display;
                            tipLines.push(`${key}: ${truncated}`);
                        }
                    }
                    if (as.source) tipLines.push(`source: ${as.source}`);
                    if (as.actors?.length > 0) tipLines.push(`actors: ${as.actors.join(', ')}`);
                    row.dataset.tooltip = tipLines.join('\n') || `${subjects} is ${predicates} of ${contexts}`;

                    // Click → open as window glyph
                    row.addEventListener('click', () => {
                        openAttestationWindow(as);
                    });
                    membersContainer.appendChild(row);
                }
                tooltip.attach(membersContainer, '.has-tooltip');
            }
        }
    } catch { membersContainer.textContent = 'Failed to load'; }

    // Cluster history — filter timeline data for this cluster
    const tlContainer = embeddingsElement.querySelector('.emb-detail-timeline') as HTMLElement;
    const clusterTimeline = timelineData.filter(p => p.cluster_id === clusterID);
    if (clusterTimeline.length >= 2) {
        renderClusterHistoryChart(tlContainer, clusterTimeline);
    } else if (clusterTimeline.length === 1) {
        tlContainer.innerHTML = `<div style="font-size:12px;color:#9ca3af">First seen: ${new Date(clusterTimeline[0].run_time).toLocaleString()} (${clusterTimeline[0].n_members} members)</div>`;
    } else {
        tlContainer.innerHTML = '<div style="font-size:12px;color:#6b7280">No history available</div>';
    }
}

function openAttestationWindow(attestation: any): void {
    const id = `as-win-${attestation.id || crypto.randomUUID()}`;
    if (glyphRun.has(id)) {
        glyphRun.openGlyph(id);
        return;
    }

    const subjects = attestation.subjects?.join(', ') || '?';
    const predicates = attestation.predicates?.join(', ') || '?';
    const titleText = `${subjects} is ${predicates}`;

    glyphRun.add({
        id,
        title: titleText,
        renderContent: () => {
            const content = document.createElement('div');
            content.style.padding = '8px';
            content.style.fontSize = '11px';
            content.style.fontFamily = 'monospace';
            content.style.color = '#e2e8f0';
            content.style.wordBreak = 'break-word';
            content.style.overflowWrap = 'break-word';

            const lines: string[] = [];
            lines.push(`<div style="margin-bottom:6px"><span style="color:#60a5fa">${escapeHtml(attestation.subjects?.join(', ') || '')}</span> <span style="color:#9ca3af">is</span> <span style="color:#4ade80">${escapeHtml(attestation.predicates?.join(', ') || '')}</span> <span style="color:#9ca3af">of</span> <span style="color:#c084fc">${escapeHtml(attestation.contexts?.join(', ') || '')}</span></div>`);

            if (attestation.actors?.length > 0) {
                lines.push(`<div style="color:#9ca3af">actors: ${escapeHtml(attestation.actors.join(', '))}</div>`);
            }
            if (attestation.source) {
                lines.push(`<div style="color:#9ca3af">source: ${escapeHtml(attestation.source)}</div>`);
            }
            if (attestation.id) {
                lines.push(`<div style="color:#6b7280;font-size:10px;margin-top:4px">${escapeHtml(attestation.id)}</div>`);
            }

            // Attributes
            if (attestation.attributes && typeof attestation.attributes === 'object') {
                lines.push('<div style="margin-top:6px;border-top:1px solid #333;padding-top:6px">');
                for (const [key, value] of Object.entries(attestation.attributes)) {
                    if (key === 'rich_string_fields') continue;
                    const display = typeof value === 'string' ? value : JSON.stringify(value);
                    lines.push(`<div><span style="color:#fbbf24">${escapeHtml(key)}:</span> ${escapeHtml(display)}</div>`);
                }
                lines.push('</div>');
            }

            content.innerHTML = lines.join('');
            return content;
        },
        initialWidth: '420px',
        initialHeight: '300px',
    });
    glyphRun.openGlyph(id);
}

function renderScatterHighlighted(container: HTMLElement, data: ProjectionPoint[], highlightCluster: number): void {
    const width = 280;
    const height = 220;
    const pad = 12;

    const svg = d3.select(container)
        .append('svg')
        .attr('width', width)
        .attr('height', height)
        .style('background', '#1e293b')
        .style('border-radius', '4px');

    const xExtent = d3.extent(data, d => d.x) as [number, number];
    const yExtent = d3.extent(data, d => d.y) as [number, number];

    const xScale = d3.scaleLinear().domain(xExtent).range([pad, width - pad]);
    const yScale = d3.scaleLinear().domain(yExtent).range([height - pad, pad]);

    const color = d3.scaleOrdinal(d3.schemeTableau10);

    // Dim all points first
    svg.selectAll('circle')
        .data(data)
        .join('circle')
        .attr('cx', d => xScale(d.x))
        .attr('cy', d => yScale(d.y))
        .attr('r', d => d.cluster_id === highlightCluster ? 4 : 2)
        .attr('fill', d => d.cluster_id === highlightCluster ? color(String(d.cluster_id)) : '#374151')
        .attr('opacity', d => d.cluster_id === highlightCluster ? 1.0 : 0.2);
}

function renderClusterHistoryChart(container: HTMLElement, data: TimelinePoint[]): void {
    const width = container.clientWidth || 600;
    const height = 120;
    const margin = { top: 8, right: 8, bottom: 24, left: 36 };
    const innerW = width - margin.left - margin.right;
    const innerH = height - margin.top - margin.bottom;

    const points = data.map(p => ({ time: new Date(p.run_time), members: p.n_members, event: p.event_type }));

    const svg = d3.select(container)
        .append('svg')
        .attr('width', width)
        .attr('height', height)
        .style('background', '#1e293b')
        .style('border-radius', '4px');

    const g = svg.append('g').attr('transform', `translate(${margin.left},${margin.top})`);

    const xScale = d3.scaleTime()
        .domain(d3.extent(points, d => d.time) as [Date, Date])
        .range([0, innerW]);

    const yScale = d3.scaleLinear()
        .domain([0, d3.max(points, d => d.members) ?? 0])
        .nice()
        .range([innerH, 0]);

    // Area
    const area = d3.area<typeof points[0]>()
        .x(d => xScale(d.time))
        .y0(innerH)
        .y1(d => yScale(d.members))
        .curve(d3.curveMonotoneX);

    g.append('path')
        .datum(points)
        .attr('d', area)
        .attr('fill', '#3b82f6')
        .attr('opacity', 0.3);

    // Line
    const line = d3.line<typeof points[0]>()
        .x(d => xScale(d.time))
        .y(d => yScale(d.members))
        .curve(d3.curveMonotoneX);

    g.append('path')
        .datum(points)
        .attr('d', line)
        .attr('fill', 'none')
        .attr('stroke', '#3b82f6')
        .attr('stroke-width', 1.5);

    // Event markers
    for (const p of points) {
        if (p.event === 'birth') {
            g.append('circle')
                .attr('cx', xScale(p.time))
                .attr('cy', yScale(p.members))
                .attr('r', 4)
                .attr('fill', '#4ade80');
        }
    }

    // Axes
    g.append('g')
        .attr('transform', `translate(0,${innerH})`)
        .call(d3.axisBottom(xScale).ticks(4).tickFormat(d => {
            const date = d as Date;
            return `${date.getMonth() + 1}/${date.getDate()}`;
        }))
        .selectAll('text').style('fill', '#9ca3af').style('font-size', '9px');

    g.append('g')
        .call(d3.axisLeft(yScale).ticks(3))
        .selectAll('text').style('fill', '#9ca3af').style('font-size', '9px');

    g.selectAll('.domain').attr('stroke', '#374151');
    g.selectAll('.tick line').attr('stroke', '#374151');
}

async function reembedAll(): Promise<void> {
    if (embeddingsReembedding || !embeddingsInfo?.available) return;

    embeddingsReembedding = true;
    renderEmbeddings();

    const resultEl = embeddingsElement?.querySelector('.emb-result');

    try {
        const ids = embeddingsInfo?.unembedded_ids ?? [];
        if (ids.length === 0) return;

        const resp = await apiFetch('/api/embeddings/batch', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ attestation_ids: ids })
        });
        const result = await resp.json();

        if (resultEl) {
            resultEl.textContent = `${result.processed} embedded, ${result.failed} failed (${result.time_ms.toFixed(0)}ms)`;
        }

        await fetchEmbeddingsInfo();
    } catch (err) {
        if (resultEl) {
            resultEl.textContent = `Error: ${err}`;
        }
    } finally {
        embeddingsReembedding = false;
        renderEmbeddings();
    }
}

async function recluster(): Promise<void> {
    if (embeddingsClustering || !embeddingsInfo?.available) return;

    embeddingsClustering = true;
    renderEmbeddings();

    const resultEl = embeddingsElement?.querySelector('.emb-cluster-result');

    try {
        const minClusterSize = Number((embeddingsElement?.querySelector('.emb-param-min-cluster-size') as HTMLInputElement)?.value) || 5;
        const clusterThreshold = Number((embeddingsElement?.querySelector('.emb-param-cluster-threshold') as HTMLInputElement)?.value) || 0.5;
        const clusterMatchThreshold = Number((embeddingsElement?.querySelector('.emb-param-match-threshold') as HTMLInputElement)?.value) || 0.7;
        const resp = await apiFetch('/api/embeddings/cluster', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ min_cluster_size: minClusterSize, cluster_threshold: clusterThreshold, cluster_match_threshold: clusterMatchThreshold })
        });
        const result = await resp.json();

        if (resultEl) {
            resultEl.textContent = `${result.summary.n_clusters} clusters, ${result.summary.n_noise} noise (${result.time_ms.toFixed(0)}ms)`;
        }

        await fetchEmbeddingsInfo();
    } catch (err) {
        if (resultEl) {
            resultEl.textContent = `Error: ${err}`;
        }
    } finally {
        embeddingsClustering = false;
        renderEmbeddings();
    }
}

function renderScatter(container: HTMLElement, data: ProjectionPoint[]): void {
    const width = 155;
    const height = 180;
    const pad = 8;

    const svg = d3.select(container)
        .append('svg')
        .attr('width', width)
        .attr('height', height)
        .style('background', '#1e293b')
        .style('border-radius', '4px');

    const xExtent = d3.extent(data, d => d.x) as [number, number];
    const yExtent = d3.extent(data, d => d.y) as [number, number];

    const xScale = d3.scaleLinear().domain(xExtent).range([pad, width - pad]);
    const yScale = d3.scaleLinear().domain(yExtent).range([height - pad, pad]);

    const color = d3.scaleOrdinal(d3.schemeTableau10);

    svg.selectAll('circle')
        .data(data)
        .join('circle')
        .attr('cx', d => xScale(d.x))
        .attr('cy', d => yScale(d.y))
        .attr('r', 3)
        .attr('fill', d => d.cluster_id === -1 ? '#6b7280' : color(String(d.cluster_id)))
        .attr('opacity', d => d.cluster_id === -1 ? 0.35 : 0.85);
}

function renderTimeline(container: HTMLElement, data: TimelinePoint[]): void {
    const width = container.clientWidth || 680;
    const height = 320;
    const margin = { top: 12, right: 12, bottom: 28, left: 42 };
    const innerW = width - margin.left - margin.right;
    const innerH = height - margin.top - margin.bottom;

    // Group data by run → { runTime, noise, clusters: {id → n_members} }
    const runMap = new Map<string, { time: Date; noise: number; clusters: Map<number, number>; events: Map<number, string> }>();
    for (const p of data) {
        let entry = runMap.get(p.run_id);
        if (!entry) {
            entry = { time: new Date(p.run_time), noise: p.n_noise, clusters: new Map(), events: new Map() };
            runMap.set(p.run_id, entry);
        }
        entry.clusters.set(p.cluster_id, p.n_members);
        if (p.event_type) entry.events.set(p.cluster_id, p.event_type);
    }

    const runs = Array.from(runMap.entries())
        .sort((a, b) => a[1].time.getTime() - b[1].time.getTime());

    // Collect all cluster IDs
    const allClusterIDs = Array.from(new Set(data.map(p => p.cluster_id))).sort((a, b) => a - b);

    // Build stacked data: each run is a row with noise + per-cluster member counts
    type StackRow = { time: Date; noise: number; [key: string]: number | Date };
    const stackData: StackRow[] = runs.map(([, entry]) => {
        const row: StackRow = { time: entry.time, noise: entry.noise };
        for (const cid of allClusterIDs) {
            row[`c${cid}`] = entry.clusters.get(cid) ?? 0;
        }
        return row;
    });

    const keys = ['noise', ...allClusterIDs.map(id => `c${id}`)];
    const stack = d3.stack<StackRow>().keys(keys).value((d, key) => {
        if (key === 'noise') return d.noise as number;
        return (d[key] as number) ?? 0;
    });
    const series = stack(stackData);

    const color = d3.scaleOrdinal(d3.schemeTableau10);
    const colorFn = (key: string) => key === 'noise' ? '#4b5563' : color(key);

    const xScale = d3.scaleTime()
        .domain(d3.extent(stackData, d => d.time) as [Date, Date])
        .range([0, innerW]);

    const yMax = d3.max(series, s => d3.max(s, d => d[1])) ?? 0;
    const yScale = d3.scaleLinear().domain([0, yMax]).nice().range([innerH, 0]);

    const svg = d3.select(container)
        .append('svg')
        .attr('width', width)
        .attr('height', height)
        .style('background', '#1e293b')
        .style('border-radius', '4px');

    const g = svg.append('g').attr('transform', `translate(${margin.left},${margin.top})`);

    // Area generator
    const area = d3.area<d3.SeriesPoint<StackRow>>()
        .x(d => xScale(d.data.time))
        .y0(d => yScale(d[0]))
        .y1(d => yScale(d[1]))
        .curve(d3.curveMonotoneX);

    // Draw stacked areas
    g.selectAll('.area')
        .data(series)
        .join('path')
        .attr('class', 'area')
        .attr('d', area)
        .attr('fill', d => colorFn(d.key))
        .attr('opacity', d => d.key === 'noise' ? 0.4 : 0.75);

    // X axis
    g.append('g')
        .attr('transform', `translate(0,${innerH})`)
        .call(d3.axisBottom(xScale).ticks(Math.min(runs.length, 6)).tickFormat(d => {
            const date = d as Date;
            return `${date.getMonth() + 1}/${date.getDate()} ${String(date.getHours()).padStart(2, '0')}:${String(date.getMinutes()).padStart(2, '0')}`;
        }))
        .selectAll('text')
        .style('fill', '#9ca3af')
        .style('font-size', '9px');

    g.selectAll('.tick line').attr('stroke', '#374151');
    g.select('.domain').attr('stroke', '#374151');

    // Y axis
    g.append('g')
        .call(d3.axisLeft(yScale).ticks(4))
        .selectAll('text')
        .style('fill', '#9ca3af')
        .style('font-size', '9px');

    g.selectAll('.tick line').attr('stroke', '#374151');
    g.selectAll('.domain').attr('stroke', '#374151');

    // Birth/death markers
    for (const [, entry] of runs) {
        for (const [cid, eventType] of entry.events) {
            const key = `c${cid}`;
            const seriesIdx = keys.indexOf(key);
            if (seriesIdx < 0) continue;
            const s = series[seriesIdx];
            const pt = s.find(d => d.data.time.getTime() === entry.time.getTime());
            if (!pt) continue;
            const cx = xScale(entry.time);
            const cy = yScale((pt[0] + pt[1]) / 2);

            if (eventType === 'birth') {
                g.append('path')
                    .attr('d', d3.symbol().type(d3.symbolTriangle).size(30)())
                    .attr('transform', `translate(${cx},${cy})`)
                    .attr('fill', '#4ade80')
                    .attr('opacity', 0.8);
            } else if (eventType === 'death') {
                g.append('path')
                    .attr('d', d3.symbol().type(d3.symbolCross).size(30)())
                    .attr('transform', `translate(${cx},${cy})`)
                    .attr('fill', '#f87171')
                    .attr('opacity', 0.8);
            }
        }
    }

    // Tooltip — invisible overlay rects per run column
    const tooltip = d3.select(container)
        .append('div')
        .style('position', 'absolute')
        .style('background', '#0f172a')
        .style('border', '1px solid #374151')
        .style('border-radius', '4px')
        .style('padding', '4px 8px')
        .style('font-size', '11px')
        .style('color', '#e2e8f0')
        .style('pointer-events', 'none')
        .style('opacity', '0')
        .style('z-index', '10');

    container.style.position = 'relative';

    // Compute per-run hover bands using midpoints between adjacent runs
    const runXs = runs.map(([, entry]) => xScale(entry.time));
    g.selectAll('.hover-rect')
        .data(runs)
        .join('rect')
        .attr('class', 'hover-rect')
        .attr('x', (_, i) => {
            const left = i === 0 ? 0 : (runXs[i - 1] + runXs[i]) / 2;
            return left;
        })
        .attr('y', 0)
        .attr('width', (_, i) => {
            const left = i === 0 ? 0 : (runXs[i - 1] + runXs[i]) / 2;
            const right = i === runs.length - 1 ? innerW : (runXs[i] + runXs[i + 1]) / 2;
            return right - left;
        })
        .attr('height', innerH)
        .attr('fill', 'transparent')
        .on('mouseover', (event: MouseEvent, [, entry]) => {
            const lines: string[] = [];
            for (const cid of allClusterIDs) {
                const n = entry.clusters.get(cid) ?? 0;
                if (n === 0) continue;
                const label = clusterLabels.get(cid);
                const name = label ? `${label} (#${cid})` : `Cluster #${cid}`;
                const ev = entry.events.get(cid);
                lines.push(`${name}: ${n}${ev && ev !== 'stable' ? ` (${ev})` : ''}`);
            }
            lines.push(`<span style="color:#6b7280">Noise: ${entry.noise}</span>`);
            tooltip.html(lines.join('<br>'))
                .style('opacity', '1')
                .style('left', `${(event as MouseEvent).offsetX + 10}px`)
                .style('top', `${(event as MouseEvent).offsetY - 10}px`);
        })
        .on('mousemove', (event: MouseEvent) => {
            tooltip
                .style('left', `${event.offsetX + 10}px`)
                .style('top', `${event.offsetY - 10}px`);
        })
        .on('mouseout', () => {
            tooltip.style('opacity', '0');
        });
}

async function projectAll(): Promise<void> {
    if (embeddingsProjecting || !embeddingsInfo?.available) return;

    embeddingsProjecting = true;
    renderEmbeddings();

    const resultEl = embeddingsElement?.querySelector('.emb-project-result');

    try {
        const nNeighbors = Number((embeddingsElement?.querySelector('.emb-param-n-neighbors') as HTMLInputElement)?.value) || 15;
        const minDist = Number((embeddingsElement?.querySelector('.emb-param-min-dist') as HTMLInputElement)?.value) || 0.1;
        const perplexity = Number((embeddingsElement?.querySelector('.emb-param-perplexity') as HTMLInputElement)?.value) || 30;
        const resp = await apiFetch('/api/embeddings/project', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ n_neighbors: nNeighbors, min_dist: minDist, perplexity }),
        });
        const result = await resp.json();

        if (resultEl) {
            const methods = (result.results || []).map((r: any) => `${r.method}:${r.n_points}pts`).join(', ');
            resultEl.textContent = `${methods} (${result.total_ms?.toFixed(0) ?? '?'}ms)`;
        }

        await fetchEmbeddingsInfo();
    } catch (err) {
        if (resultEl) {
            resultEl.textContent = `Error: ${err}`;
        }
    } finally {
        embeddingsProjecting = false;
        renderEmbeddings();
    }
}

// Register default system glyphs
export function registerDefaultGlyphs(): void {
    // Canvas Glyph - Fractal container with spatial grid
    glyphRun.add(createCanvasGlyph());

    // Database Statistics Glyph
    glyphRun.add({
        id: 'database-glyph',
        title: `${DB} Database Statistics`,
        renderContent: () => {
            const content = document.createElement('div');
            dbStatsElement = content;
            sendMessage({ type: 'get_database_stats' });
            renderDbStats();
            return content;
        },
        initialWidth: '400px',
        initialHeight: '240px',
        defaultX: 100,
        defaultY: 100
    });

    // Embeddings Glyph
    glyphRun.add({
        id: 'embeddings-glyph',
        title: '\u29C9 Embeddings',
        renderContent: () => {
            const content = document.createElement('div');
            embeddingsElement = content;
            fetchEmbeddingsInfo();
            return content;
        },
        initialWidth: '720px',
        initialHeight: '820px',
    });

    // Sync Status Glyph
    browserSync.onStateChange((state) => {
        browserState = state;
        if (syncElement) renderSync();
    });

    glyphRun.add({
        id: 'sync-glyph',
        title: '↔ Sync',
        renderContent: () => {
            const content = document.createElement('div');
            syncElement = content;
            sendMessage({ type: 'get_sync_status' });
            renderSync();
            return content;
        },
        initialWidth: '420px',
        initialHeight: '360px',
        defaultX: 120,
        defaultY: 120
    });

    // Self Diagnostics Glyph
    glyphRun.add({
        id: 'self-glyph',
        title: '⍟ Self',
        renderContent: () => {
            const content = document.createElement('div');
            selfElement = content;
            renderSelf();
            return content;
        },
        initialWidth: '450px',
        initialHeight: '320px'
    });

    // Usage & Cost Chart Glyph
    // TODO(future): Budget alerting with notifications
    // Implement cost threshold monitoring with user notifications:
    // - Config: User-defined budget limits (daily/weekly/monthly)
    // - Detection: Check total cost vs. budget in chart render
    // - Notification: Toast alert when threshold crossed
    // - Persistence: Store alert state to avoid repeat notifications
    // - UX: Clear visual indication of budget status in chart
    glyphRun.add(createChartGlyph(
        'usage-chart',
        '$ Usage & Costs',
        '/api/timeseries/usage',
        {
            primaryField: 'cost',
            secondaryField: 'requests',
            primaryLabel: 'Cost',
            secondaryLabel: 'Requests',
            primaryColor: '#4ade80',
            secondaryColor: '#60a5fa',
            chartType: 'area',
            formatValue: (v) => `$${v.toFixed(2)}`,
            defaultRange: 'month'
        }
    ));

    // Plugin Panel Glyph — panel manifestation
    glyphRun.add(createPluginGlyph());

    log.debug(SEG.UI, 'Default glyphs registered:', {
        canvas: 'Spatial canvas grid',
        database: 'Database statistics',
        embeddings: 'Embedding service status',
        sync: 'Attestation sync status',
        self: 'Self diagnostics',
        usage: 'API usage and costs',
        plugins: 'Domain plugin panel'
    });
}