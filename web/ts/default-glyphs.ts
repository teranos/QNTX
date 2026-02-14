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

import { glyphRun } from './components/glyph/run';
import { createCanvasGlyph } from './components/glyph/canvas/canvas-glyph';
import { createChartGlyph } from './components/glyph/chart-glyph';
import { sendMessage } from './websocket';
import { apiFetch } from './api';
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
            const statusColor = p.status === 'ok' ? '#4ade80' : p.status === 'unreachable' ? '#f87171' : '#6b7280';
            const statusDot = p.status ? `<span style="color: ${statusColor}; margin-right: 6px;">●</span>` : '';
            return `
            <div class="glyph-row" style="align-items: center;">
                ${statusDot}<span class="glyph-label">${p.name}:</span>
                <span class="glyph-value" style="font-size: 11px; flex: 1;">${p.url}</span>
                <button class="sync-peer-btn" data-peer-url="${p.url}" style="
                    background: transparent; border: 1px solid #60a5fa; color: #60a5fa;
                    padding: 2px 8px; border-radius: 3px; cursor: pointer;
                    font-family: monospace; font-size: 11px; margin-left: 8px;
                ">Sync</button>
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
let embeddingsInfo: { available: boolean; model_name: string; dimensions: number; embedding_count: number; attestation_count: number; unembedded_ids?: string[] } | null = null;
let embeddingsReembedding = false;

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
        const resp = await apiFetch('/api/embeddings/info');
        embeddingsInfo = await resp.json();
    } catch {
        embeddingsInfo = null;
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
        </div>
    `;

    const btn = embeddingsElement.querySelector('.emb-reembed-btn');
    if (btn) {
        btn.addEventListener('click', reembedAll);
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
        initialWidth: '360px',
        initialHeight: '200px'
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