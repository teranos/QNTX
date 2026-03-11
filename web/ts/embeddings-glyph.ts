import * as d3 from 'd3';
import { apiFetch } from './api';
import { escapeHtml } from './html-utils';
import { glyphRun } from './components/glyph/run';
import { tooltip } from './components/tooltip';
import { log, SEG } from './logger';

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
                <button class="emb-back-btn panel-btn" style="padding:2px 10px;font-size:12px">\u2190 Back</button>
                ${prevID !== null ? `<button class="emb-prev-btn panel-btn" style="padding:2px 8px;font-size:12px">\u2190</button>` : ''}
                <span style="font-weight:bold;color:${color}">${title}</span>
                <span style="color:#9ca3af;font-size:12px">${memberCount} members</span>
                ${nextID !== null ? `<button class="emb-next-btn panel-btn" style="padding:2px 8px;font-size:12px">\u2192</button>` : ''}
                <span style="color:#6b7280;font-size:10px;margin-left:auto">${idx + 1}/${clusterIDs.length} \u2190 \u2192</span>
            </div>

            <div class="glyph-section" style="margin-top:4px">
                <h3 class="glyph-section-title">Projection</h3>
                <div class="emb-detail-scatters" style="display:flex;gap:6px"></div>
            </div>

            <div class="glyph-section" style="margin-top:8px;padding-top:8px;border-top:1px solid var(--border-color, #333)">
                <h3 class="glyph-section-title">Cluster History</h3>
                <div class="emb-detail-timeline"></div>
            </div>

            <div class="glyph-section" style="margin-top:8px;padding-top:8px;border-top:1px solid var(--border-color, #333)">
                <h3 class="glyph-section-title">Recent Attestations</h3>
                <div class="emb-detail-members" style="max-height:300px;overflow-y:auto">
                    <div class="glyph-loading">Loading...</div>
                </div>
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

    // Prev/next navigation
    if (prevID !== null) {
        embeddingsElement.querySelector('.emb-prev-btn')?.addEventListener('click', () => renderClusterDetail(prevID));
    }
    if (nextID !== null) {
        embeddingsElement.querySelector('.emb-next-btn')?.addEventListener('click', () => renderClusterDetail(nextID));
    }

    // Keyboard navigation: left/right arrows, Escape to go back
    clusterDetailKeyHandler = (e: KeyboardEvent) => {
        if (e.key === 'ArrowLeft' && prevID !== null) {
            renderClusterDetail(prevID);
        } else if (e.key === 'ArrowRight' && nextID !== null) {
            renderClusterDetail(nextID);
        } else if (e.key === 'Escape') {
            cleanupAndBack();
        }
    };
    document.addEventListener('keydown', clusterDetailKeyHandler);

    // Projection scatter — highlight this cluster, dim rest
    const scatterContainer = embeddingsElement.querySelector('.emb-detail-scatters') as HTMLElement;
    if (scatterContainer) {
        for (const method of Object.keys(projectionsData)) {
            const pts = projectionsData[method];
            if (!pts?.length) continue;
            const wrapper = document.createElement('div');
            wrapper.style.flex = '1';
            wrapper.style.minWidth = '0';
            const methodLabel = document.createElement('div');
            methodLabel.style.fontSize = '11px';
            methodLabel.style.color = '#9ca3af';
            methodLabel.style.textAlign = 'center';
            methodLabel.style.marginBottom = '4px';
            methodLabel.textContent = method.toUpperCase();
            wrapper.appendChild(methodLabel);
            const canvas = document.createElement('div');
            wrapper.appendChild(canvas);
            scatterContainer.appendChild(wrapper);
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
        onClose: () => glyphRun.remove(id),
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
    const ttip = d3.select(container)
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
            ttip.html(lines.join('<br>'))
                .style('opacity', '1')
                .style('left', `${(event as MouseEvent).offsetX + 10}px`)
                .style('top', `${(event as MouseEvent).offsetY - 10}px`);
        })
        .on('mousemove', (event: MouseEvent) => {
            ttip
                .style('left', `${event.offsetX + 10}px`)
                .style('top', `${event.offsetY - 10}px`);
        })
        .on('mouseout', () => {
            ttip.style('opacity', '0');
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

export function createEmbeddingsGlyph() {
    return {
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
    };
}
