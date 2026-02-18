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
import { formatBuildTime } from './components/tooltip.ts';
import type { VersionMessage, SystemCapabilitiesMessage, SyncStatusMessage } from '../types/websocket';

// Sync status state
let syncElement: HTMLElement | null = null;
let syncStatus: SyncStatusMessage | null = null;

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
            const advertisedName = p.advertised_name ? ` <span style="color: #9ca3af;">(${p.advertised_name})</span>` : '';
            return `
            <div class="glyph-row" style="align-items: center;">
                ${statusDot}<span class="glyph-label">${p.name}${advertisedName}:</span>
                <span class="glyph-value" style="font-size: 11px; flex: 1;">${p.url}</span>
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
                    <span class="glyph-label">Root:</span>
                    <span class="glyph-value" style="color: ${rootColor}; font-family: monospace; font-size: 12px;">${rootShort}</span>
                </div>
                <div class="glyph-row">
                    <span class="glyph-label">Groups:</span>
                    <span class="glyph-value">${(syncStatus.groups ?? 0).toLocaleString()} <span style="color: #6b7280;">(actor, context) pairs</span></span>
                </div>
            </div>
            ${peersSection}
            ${visionSection}
        </div>
    `;

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
} | null = null;
let embeddingsReembedding = false;
let embeddingsClustering = false;
let embeddingsProjecting = false;
type ProjectionPoint = { id: string; source_id: string; method: string; x: number; y: number; cluster_id: number };
let projectionsData: Record<string, ProjectionPoint[]> = {};
let clusterLabels: Map<number, string | null> = new Map();
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
            const clusterSizes = Object.entries(ci.clusters)
                .sort(([a], [b]) => Number(a) - Number(b))
                .map(([id, count]) => {
                    const label = clusterLabels.get(Number(id));
                    const labelSpan = label ? ` <span style="color:#a0aec0;font-style:italic">${escapeHtml(label)}</span>` : '';
                    return `<span style="color:#60a5fa">#${id}</span>${labelSpan}:${count}`;
                })
                .join('  ');
            clusterRows = `
                <div class="glyph-row">
                    <span class="glyph-label">Clusters:</span>
                    <span class="glyph-value">${ci.n_clusters}</span>
                </div>
                <div class="glyph-row">
                    <span class="glyph-label">Noise:</span>
                    <span class="glyph-value">${ci.n_noise}</span>
                </div>
                <div class="glyph-row">
                    <span class="glyph-label">Sizes:</span>
                    <span class="glyph-value" style="font-size:11px;flex-wrap:wrap;display:flex;gap:2px 8px">${clusterSizes}</span>
                </div>
            `;
        } else {
            clusterRows = `
                <div class="glyph-row">
                    <span class="glyph-label">Clusters:</span>
                    <span class="glyph-value" style="color:#6b7280">not computed</span>
                </div>
            `;
        }
        clusterSection = `
            <div class="glyph-section" style="margin-top:8px;padding-top:8px;border-top:1px solid var(--border-color, #333)">
                <h3 class="glyph-section-title">HDBSCAN Clustering</h3>
                ${clusterRows}
                <button class="emb-cluster-btn panel-btn" style="width:100%;margin-top:6px"
                    ${embeddingsClustering ? 'disabled' : ''}>
                    ${embeddingsClustering ? 'Clustering...' : 'Recompute Clusters'}
                </button>
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
            const scatterSlots = methodNames
                .filter(m => projectionsData[m]?.length > 0)
                .map(m => {
                    const pts = projectionsData[m];
                    const nClusters = new Set(pts.filter(p => p.cluster_id !== -1).map(p => p.cluster_id)).size;
                    return `<div style="flex:1;min-width:0">
                        <div style="font-size:11px;color:#9ca3af;text-align:center;margin-bottom:4px">${m.toUpperCase()} (${pts.length}pts, ${nClusters}cl)</div>
                        <div class="emb-scatter" data-method="${m}"></div>
                    </div>`;
                }).join('');
            scatterSection = `
                <div class="glyph-section" style="margin-top:8px;padding-top:8px;border-top:1px solid var(--border-color, #333)">
                    <h3 class="glyph-section-title">Projections</h3>
                    <div style="display:flex;gap:6px">${scatterSlots}</div>
                    <button class="emb-project-btn panel-btn" style="width:100%;margin-top:6px"
                        ${embeddingsProjecting ? 'disabled' : ''}>
                        ${embeddingsProjecting ? 'Projecting...' : 'Re-project'}
                    </button>
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
                    <button class="emb-project-btn panel-btn" style="width:100%;margin-top:6px"
                        ${embeddingsProjecting ? 'disabled' : ''}>
                        ${embeddingsProjecting ? 'Projecting...' : 'Project'}
                    </button>
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
        const resp = await apiFetch('/api/embeddings/cluster', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ min_cluster_size: 5 })
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
        const resp = await apiFetch('/api/embeddings/project', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({}),
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

    log.debug(SEG.UI, 'Default glyphs registered:', {
        canvas: 'Spatial canvas grid',
        database: 'Database statistics',
        embeddings: 'Embedding service status',
        sync: 'Attestation sync status',
        self: 'Self diagnostics',
        usage: 'API usage and costs'
    });
}