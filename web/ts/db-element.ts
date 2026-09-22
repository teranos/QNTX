import { sendMessage } from './client';
import { log, SEG } from './logger';
import { escapeHtml } from './html-utils';
import { DB, Watcher } from './sym';
import { seedEvictions, recordEviction as recordEvictionEvent, getEvictionSummary, hasEvictions, renderEvictionChart, getPredicateBreakdown, type PredicateDetail } from './eviction-chart';
import { getWatchersByPredicate, setDilation, eyeStyle } from './watcher-predicates';
import type { Element } from '@teranos/elements';

let dbStatsElement: HTMLElement | null = null;
let dbStats: any = null;

// Chart viewport: indices into sorted allKeys array
let chartViewStart = 0;
let chartViewEnd = 0; // 0 = will be set to allKeys.length on first render
let allSortedKeys: string[] = [];

// Section containers (created once, survive re-renders)
let sectionOverview: HTMLElement | null = null;
let sectionChart: HTMLElement | null = null;
let sectionPredicates: HTMLElement | null = null;
let sectionEvictions: HTMLElement | null = null;
let sectionPerformance: HTMLElement | null = null;

export function updateDatabaseStats(stats: any): void {
    dbStats = stats;
    if (stats.recent_evictions) {
        seedEvictions(stats.recent_evictions);
    }
    if (dbStatsElement) {
        renderDbStats();
    }
}

export function recordEviction(data: { event_type: string; actor: string; context: string; entity: string; deletions_count: number; message: string }): void {
    recordEvictionEvent(data);
    if (dbStatsElement) {
        renderDbStats();
    }
}

function createSections(root: HTMLElement): void {
    sectionChart = document.createElement('div');
    sectionChart.className = 'db-section-chart';

    sectionOverview = document.createElement('div');
    sectionOverview.className = 'db-section-overview';

    // 3-column grid below the chart
    const columns = document.createElement('div');
    columns.style.cssText = 'display: grid; grid-template-columns: 1fr 1fr 1fr; gap: 16px; margin-top: 8px;';

    sectionPredicates = document.createElement('div');
    sectionPredicates.className = 'db-section-predicates';

    sectionEvictions = document.createElement('div');
    sectionEvictions.className = 'db-section-evictions';

    sectionPerformance = document.createElement('div');
    sectionPerformance.className = 'db-section-performance';

    columns.appendChild(sectionPredicates);
    columns.appendChild(sectionEvictions);
    columns.appendChild(sectionPerformance);

    // Chart is the hero — full width at the top
    root.appendChild(sectionChart);
    root.appendChild(sectionOverview);
    root.appendChild(columns);
}

// Color palette for predicate lines
const PREDICATE_COLORS = [
    '#4ade80', // green
    '#60a5fa', // blue
    '#f59e0b', // amber
    '#ef4444', // red
    '#a78bfa', // purple
    '#f472b6', // pink
    '#2dd4bf', // teal
    '#fb923c', // orange
    '#818cf8', // indigo
    '#34d399', // emerald
];

// renderStatsError shows the whole envelope: what failed, where, every detail
// and hint the error chain carried, and the id it is logged under.
function renderStatsError(err: any): string {
    if (typeof err === 'string') {
        return `<div class="element-error">${escapeHtml(err)}</div>`;
    }
    const rows = [
        `<div class="element-error"><span class="label">${escapeHtml(err.surface ?? 'database stats')}</span> ${escapeHtml(err.error ?? 'failed')}</div>`,
    ];
    for (const detail of err.details ?? []) {
        rows.push(`<div class="element-error-detail">${escapeHtml(detail)}</div>`);
    }
    for (const hint of err.hints ?? []) {
        rows.push(`<div class="element-error-hint">${escapeHtml(hint)}</div>`);
    }
    if (err.id) {
        rows.push(`<div class="element-error-id">${escapeHtml(err.id)}</div>`);
    }
    return rows.join('');
}

interface Landing {
    namespace: string;
    path: string;
    bytes: number;
    wal_bytes: number;
    attestations: number;
    actors: number;
    subjects: number;
    contexts: number;
    top_predicates: Common[] | null;
    top_contexts: Common[] | null;
    over: Record<string, number> | null;
}

// One line per namespace, of when its attestations landed. The chart below
// was written for distillation's histograms and drew nothing once a node
// stopped distilling; this is the same chart reading what the node holds.
function landedOverTime(landings: Landing[] | undefined): Record<string, Record<string, number>> | null {
    if (!landings || landings.length === 0) {
        return null;
    }
    const lines: Record<string, Record<string, number>> = {};
    for (const one of landings) {
        if (one.over && Object.keys(one.over).length > 0) {
            lines[one.namespace] = one.over;
        }
    }
    return Object.keys(lines).length > 0 ? lines : null;
}

interface Common {
    name: string;
    count: number;
}

// A panel is as wide as the canvas lets it be, and a row laid out across all
// of that puts its last column an arm's length from its first. These are
// tables: fixed columns, and a width a row is read across rather than scanned.
const LANDING_WIDTH = '860px';
const LANDING_COLUMNS = '110px minmax(0, 1fr) 84px 84px 132px 72px';
const SPEND_COLUMNS = '132px 92px 60px 64px';

// What a namespace is mostly about, clickable the way a type is: the same
// class and data-type the wiring below already listens for.
function commonHTML(label: string, common: Common[] | null): string {
    if (!common || common.length === 0) {
        return '';
    }
    const items = common.map(one =>
        `<span class="element-type-link" data-type="${escapeHtml(one.name)}" style="cursor: pointer; margin-right: 8px;">${escapeHtml(one.name)} <span style="color: #475569;">${one.count.toLocaleString()}</span></span>`
    ).join('');
    return `<div style="display: grid; grid-template-columns: 78px minmax(0, 1fr); gap: 6px; padding: 1px 0 3px 12px; font-size: 11px; max-width: ${LANDING_WIDTH};">
        <span style="color: #475569;">${label}</span>
        <span style="display: flex; flex-wrap: wrap; gap: 2px; color: #94a3b8;">${items}</span>
    </div>`;
}

// A read is answered from the database of its namespace and never from the
// record (ADR-037), so there is one of these per namespace and the single
// path this panel used to print was hiding all but one of them.
function landingsHTML(landings: Landing[] | undefined, failed: any, onePath: string): string {
    if (failed) {
        return renderStatsError(failed);
    }
    if (!landings || landings.length === 0) {
        return `<div style="padding: 6px 0; font-size: 11px;">
            <span class="label">Path:</span> <span class="element-value">${escapeHtml(onePath)}</span>
        </div>`;
    }

    const held = landings.reduce((sum, one) => sum + one.attestations, 0);
    const rows = landings.map(one => `
        <div style="display: grid; grid-template-columns: ${LANDING_COLUMNS}; gap: 8px; font-size: 11px; padding: 2px 0; max-width: ${LANDING_WIDTH};">
            <span style="color: #e2e8f0; overflow: hidden; text-overflow: ellipsis;">${escapeHtml(one.namespace)}</span>
            <span style="color: #475569; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; direction: rtl; text-align: left;">${escapeHtml(one.path)}</span>
            <span style="color: #64748b; text-align: right;">db ${formatBytes(one.bytes)}</span>
            <span style="color: ${one.wal_bytes > 8 * 1024 * 1024 ? '#f59e0b' : '#64748b'}; text-align: right;">wal ${formatBytes(one.wal_bytes)}</span>
            <span style="color: #475569; text-align: right;">${one.actors.toLocaleString()}a ${one.subjects.toLocaleString()}s ${one.contexts.toLocaleString()}c</span>
            <span style="color: #94a3b8; text-align: right;">${one.attestations.toLocaleString()}</span>
        </div>
        ${commonHTML('predicates', one.top_predicates)}
        ${commonHTML('contexts', one.top_contexts)}`).join('');

    return `
        <div style="padding: 8px 0; border-bottom: 1px solid var(--border-color, #333);">
            <span class="label">Answering reads from ${landings.length} ${landings.length === 1 ? 'database' : 'databases'}:</span>
            <span class="element-value">${held.toLocaleString()} attestations</span>
            <div style="margin-top: 4px;">${rows}</div>
        </div>
    `;
}

interface Spend {
    of: string;
    request: string;
    held_on_node: boolean;
    count: number;
}

// What reading the record has cost, per reader, most spent first. A backend
// holding its record on the node sends none of this and the section is absent
// — nothing left the node, so nothing was spent reading it.
function recordSpendHTML(spend: Spend[] | undefined, failed: any): string {
    if (failed) {
        return renderStatsError(failed);
    }
    if (!spend || spend.length === 0) {
        return '';
    }

    // A reader the node keeps nothing of pays S3 for every read of it
    // (ADR-037). Said on the row, because a large number there is a defect
    // and the same number beside "on the node" is the record doing its job.
    const rows = spend.map(one => `
        <div style="display: grid; grid-template-columns: ${SPEND_COLUMNS}; gap: 8px; font-size: 11px; padding: 2px 0;">
            <span style="color: ${one.held_on_node ? '#e2e8f0' : '#f59e0b'}; overflow: hidden; text-overflow: ellipsis;">${escapeHtml(one.of)}</span>
            <span style="color: ${one.held_on_node ? '#475569' : '#f59e0b'};">${one.held_on_node ? 'on the node' : 'record only'}</span>
            <span style="color: #64748b;">${escapeHtml(one.request)}</span>
            <span style="color: #94a3b8; text-align: right;">${one.count.toLocaleString()}</span>
        </div>`).join('');

    const total = spend.reduce((sum, one) => sum + one.count, 0);
    const offNode = spend.filter(one => !one.held_on_node).reduce((sum, one) => sum + one.count, 0);
    const leaving = offNode > 0
        ? ` <span style="color: #f59e0b;">${offNode.toLocaleString()} of them for things the node keeps no copy of</span>`
        : '';

    return `
        <div style="margin-bottom: 8px;">
            <span class="label">Reading the record has cost:</span>
            <span class="element-value">${total.toLocaleString()} requests</span>${leaving}
            <div style="margin-top: 4px;">${rows}</div>
        </div>
    `;
}

function renderDbStats(): void {
    if (!dbStatsElement || !sectionChart || !sectionOverview || !sectionPredicates || !sectionEvictions || !sectionPerformance) return;

    if (!dbStats) {
        sectionChart.innerHTML = `<div class="element-loading">Loading database statistics...</div>`;
        return;
    }
    if (dbStats.error) {
        sectionChart.innerHTML = renderStatsError(dbStats.error);
        return;
    }

    // -- Chart: multi-predicate timeseries with range selector --
    // The key is absent on a backend that does not distil, where a chart of
    // distillation output is not empty but meaningless.
    if ('predicate_histograms' in dbStats) {
        renderChartWithControls(sectionChart, dbStats.predicate_histograms);
    } else {
        // A node that persists to the record rather than distilling has no
        // histograms and drew nothing here. What it does have is when its
        // attestations landed, per namespace.
        renderChartWithControls(sectionChart, landedOverTime(dbStats.landings));
    }

    // -- Overview: compact stats row --
    const storageBackend = dbStats.storage_optimized
        ? `rust (optimized) v${dbStats.storage_version}`
        : 'go (fallback)';

    // A count is rendered when the payload carries it. Absent means this
    // backend does not answer it; zero is an answer and renders as one.
    const countSpan = (label: string, value: unknown): string =>
        typeof value === 'number'
            ? `<span><span class="label">${label}:</span> <span class="element-value">${value.toLocaleString()}</span></span>`
            : '';

    sectionOverview.innerHTML = `
        <div style="display: flex; flex-wrap: wrap; gap: 16px; padding: 8px 0; border-bottom: 1px solid var(--border-color, #333); font-size: 11px;">
            <span><span class="label">Backend:</span> <span class="element-value">${storageBackend}</span></span>
            ${countSpan('Attestations', dbStats.total_attestations)}
            ${countSpan('Actors', dbStats.unique_actors)}
            ${countSpan('Subjects', dbStats.unique_subjects)}
            ${countSpan('Contexts', dbStats.unique_contexts)}
        </div>
        ${landingsHTML(dbStats.landings, dbStats.landings_error, String(dbStats.path ?? ''))}
    `;

    // -- Predicates: what the record cost, then types + distillation --
    let predicatesHTML = recordSpendHTML(dbStats.record_spend, dbStats.record_spend_error);

    // Rich fields / types
    const richFields = dbStats.rich_fields;
    if (richFields && richFields.length > 0) {
        const isEnhanced = typeof richFields[0] === 'object' && 'field' in richFields[0];
        const fieldItems = isEnhanced
            ? richFields
                .sort((a: any, b: any) => b.count - a.count)
                .map((f: any) => `<span class="element-type-link" data-type="${f.field}" style="cursor: pointer; margin-right: 8px;">${f.field} (${f.count})</span>`)
                .join('')
            : richFields.sort().map((f: string) => `<span class="element-type-link" data-type="${f}" style="cursor: pointer; margin-right: 8px;">${f}</span>`).join('');

        predicatesHTML += `
            <div style="margin-bottom: 8px;">
                <span class="label">Types (${richFields.length}):</span>
                <span class="element-value" style="display: flex; flex-wrap: wrap; gap: 4px;">${fieldItems}</span>
            </div>
        `;
    }

    // Distillation summary
    if (dbStats.distillation) {
        const d = dbStats.distillation;
        const preserved = d.preserved_count ? d.preserved_count.toLocaleString() : '0';
        const oldest = d.oldest ? formatAge(d.oldest) : '';
        const newest = d.newest ? formatAge(d.newest) : '';
        const timeRange = oldest && newest ? `${oldest} - ${newest}` : '';

        predicatesHTML += `
            <div style="margin-bottom: 4px;">
                <span class="label">Distillation:</span>
                <span class="element-value">${d.sigmas} sigmas, ${preserved} original preserved</span>
                ${timeRange ? `<span style="color: #64748b; margin-left: 8px;">${timeRange}</span>` : ''}
            </div>
        `;

        // Predicate list with color indicators matching chart
        if (d.predicates && d.predicates.length > 0) {
            const watcherMap = getWatchersByPredicate();
            const predRows = d.predicates.map((p: { predicate: string; count: number }, i: number) => {
                const color = PREDICATE_COLORS[i % PREDICATE_COLORS.length];
                const info = watcherMap.get(p.predicate);
                const eyes = info ? (() => { const s = eyeStyle(info); return `<span style="color: ${s.color}; text-shadow: ${s.shadow}; cursor: default;" title="${info.names.join(', ')}">${Watcher.repeat(info.names.length)}</span>`; })() : '';
                return `<div style="display: flex; align-items: center; gap: 6px; font-size: 11px; padding: 2px 0;">
                    <span style="width: 8px; height: 8px; border-radius: 50%; background: ${color}; flex-shrink: 0;"></span>
                    <span style="color: #e2e8f0; word-break: break-word; overflow-wrap: break-word; flex: 1;">${p.predicate}${eyes}</span>
                    <span style="color: #94a3b8; white-space: nowrap;">${p.count}</span>
                </div>`;
            }).join('');
            predicatesHTML += `<div style="margin-top: 4px;">${predRows}</div>`;
        }
    }

    sectionPredicates.innerHTML = predicatesHTML ? `<div style="padding: 8px 0; border-bottom: 1px solid var(--border-color, #333);">${predicatesHTML}</div>` : '';

    // Wire type links. Both sections, because a namespace's own predicates and
    // contexts are drawn beside its database in the overview and they open the
    // same way a type does.
    for (const section of [sectionPredicates, sectionOverview]) {
        section.querySelectorAll('.element-type-link').forEach(el => {
            el.addEventListener('click', () => {
                const typeName = (el as HTMLElement).dataset.type;
                if (typeName) {
                    import('./type-definition-window.js')
                        .then(({ openTypeDefinition }) => openTypeDefinition(typeName))
                        .catch((err: unknown) => log.error(SEG.ELEMENT, `Type definition for ${typeName} failed to open:`, err));
                }
            });
        });
    }

    // -- Evictions --
    if (hasEvictions()) {
        const summary = getEvictionSummary();
        const breakdown = getPredicateBreakdown();

        let predicateRows = '';
        if (breakdown.length > 0) {
            const items = breakdown.map((b, i) => {
                const age = b.oldestEvicted ? formatAge(b.oldestEvicted) : '';
                return `<div class="eviction-pred-row" data-pred-idx="${i}" style="display: flex; justify-content: space-between; font-size: 11px; padding: 2px 0; cursor: pointer;">
                    <span style="color: #e2e8f0; word-break: break-word; overflow-wrap: break-word;">${b.predicate}</span>
                    <span style="white-space: nowrap; margin-left: 8px;">${age ? `<span style="color: #64748b; margin-right: 6px;">${age}</span>` : ''}<span style="color: #94a3b8;">${b.count.toLocaleString()}</span></span>
                </div>
                <div class="eviction-pred-detail" data-pred-detail="${i}" style="display: none;"></div>`;
            }).join('');
            predicateRows = `
                <div style="margin-top: 6px;" class="eviction-predicates-container">
                    <span class="label" style="font-size: 11px;">Evicted predicates:</span>
                    ${items}
                </div>
            `;
        }

        sectionEvictions.innerHTML = `
            <div style="padding: 8px 0; border-bottom: 1px solid var(--border-color, #333);">
                <div style="margin-bottom: 4px;">
                    <span class="label">Evictions:</span>
                    <span class="element-value">${summary.count} events, ${summary.totalEvicted.toLocaleString()} attestations evicted</span>
                </div>
                <div class="eviction-chart-container"></div>
                ${predicateRows}
            </div>
        `;

        const chartContainer = sectionEvictions.querySelector('.eviction-chart-container');
        if (chartContainer) {
            renderEvictionChart(chartContainer as HTMLElement);
        }

        // Wire predicate drill-down
        sectionEvictions.querySelectorAll('.eviction-pred-row').forEach(el => {
            el.addEventListener('click', () => {
                const idx = parseInt((el as HTMLElement).dataset.predIdx ?? '', 10);
                const detail = sectionEvictions!.querySelector(`[data-pred-detail="${idx}"]`) as HTMLElement;
                if (!detail || isNaN(idx)) return;
                if (detail.style.display === 'none') {
                    detail.style.display = 'block';
                    renderPredicateDetail(detail, breakdown[idx]);
                } else {
                    detail.style.display = 'none';
                }
            });
        });
    } else {
        sectionEvictions.innerHTML = '';
    }

    // -- Performance --
    if (dbStats.performance || dbStats.live) {
        renderPerformanceSection(sectionPerformance, dbStats.performance, dbStats.live);
    } else {
        sectionPerformance.innerHTML = '';
    }
}

function renderChartWithControls(container: HTMLElement, histograms: Record<string, Record<string, number>> | null): void {
    if (!histograms) {
        container.innerHTML = '<div style="padding: 16px; color: #64748b; font-size: 11px;">No histogram data yet (waiting for distillation)</div>';
        return;
    }

    // Build full sorted key set once
    const keySet = new Set<string>();
    for (const pred of Object.keys(histograms)) {
        for (const key of Object.keys(histograms[pred])) {
            keySet.add(key);
        }
    }
    allSortedKeys = Array.from(keySet).sort();
    if (allSortedKeys.length === 0) return;

    // Initialize viewport to show all on first render
    if (chartViewEnd === 0 || chartViewEnd > allSortedKeys.length) {
        chartViewStart = 0;
        chartViewEnd = allSortedKeys.length;
    }

    let chartArea = container.querySelector('.db-chart-area') as HTMLElement;
    if (!chartArea) {
        chartArea = document.createElement('div');
        chartArea.className = 'db-chart-area';
        chartArea.style.cursor = 'grab';
        container.appendChild(chartArea);

        // Wheel: deltaY = zoom, deltaX = pan
        chartArea.addEventListener('wheel', (e) => {
            e.preventDefault();
            const total = allSortedKeys.length;
            const span = chartViewEnd - chartViewStart;

            if (Math.abs(e.deltaY) > Math.abs(e.deltaX)) {
                // Zoom: deltaY > 0 = zoom out, < 0 = zoom in
                const zoomFactor = e.deltaY > 0 ? 1.15 : 0.85;
                const newSpan = Math.max(64, Math.min(total, Math.round(span * zoomFactor)));
                const center = (chartViewStart + chartViewEnd) / 2;
                chartViewStart = Math.max(0, Math.round(center - newSpan / 2));
                chartViewEnd = Math.min(total, chartViewStart + newSpan);
                if (chartViewStart === 0) chartViewEnd = Math.min(total, newSpan);
            } else {
                // Pan: deltaX > 0 = pan right (forward), < 0 = pan left (back)
                const panAmount = Math.max(1, Math.round(span * 0.05)) * Math.sign(e.deltaX);
                const newStart = Math.max(0, Math.min(total - span, chartViewStart + panAmount));
                chartViewEnd = newStart + span;
                chartViewStart = newStart;
            }

            renderViewport(chartArea!, histograms!);
        }, { passive: false });
    }

    renderViewport(chartArea, histograms);
}

function renderViewport(chartArea: HTMLElement, histograms: Record<string, Record<string, number>>): void {
    // Slice keys to viewport
    const viewKeys = allSortedKeys.slice(chartViewStart, chartViewEnd);
    if (viewKeys.length === 0) return;

    // Build filtered histograms for visible range
    const filtered: Record<string, Record<string, number>> = {};
    for (const [pred, hist] of Object.entries(histograms)) {
        const filt: Record<string, number> = {};
        for (const key of viewKeys) {
            if (hist[key]) filt[key] = hist[key];
        }
        if (Object.keys(filt).length > 0) {
            filtered[pred] = filt;
        }
    }

    // Show viewport indicator
    const total = allSortedKeys.length;
    const pct = ((chartViewEnd - chartViewStart) / total * 100).toFixed(0);
    const rangeLabel = viewKeys.length < total
        ? `${viewKeys[0]} \u2014 ${viewKeys[viewKeys.length - 1]} (${pct}%)`
        : 'all';

    renderTimeseriesChart(chartArea, Object.keys(filtered).length > 0 ? filtered : null, rangeLabel);
}

// Render multi-series timeseries chart from predicate histogram data
function renderTimeseriesChart(container: HTMLElement, histograms: Record<string, Record<string, number>> | null, rangeLabel?: string): void {
    if (!histograms) {
        container.innerHTML = '<div style="padding: 16px; color: #64748b; font-size: 11px;">No histogram data yet (waiting for distillation)</div>';
        return;
    }

    const predicates = Object.keys(histograms);
    if (predicates.length === 0) {
        container.innerHTML = '<div style="padding: 16px; color: #64748b; font-size: 11px;">No histogram data</div>';
        return;
    }

    // Collect all time keys across all predicates, sorted
    const allKeysSet = new Set<string>();
    for (const pred of predicates) {
        for (const key of Object.keys(histograms[pred])) {
            allKeysSet.add(key);
        }
    }
    const allKeys = Array.from(allKeysSet).sort();
    if (allKeys.length === 0) return;

    // Sort predicates by total observations descending, cap at top 10
    const predTotals = predicates.map(p => {
        let total = 0;
        for (const v of Object.values(histograms[p])) total += v;
        return { predicate: p, total };
    }).sort((a, b) => b.total - a.total);

    const topPredicates = predTotals.slice(0, 10);
    const sortedPredicates = topPredicates.map(p => p.predicate);

    // Build series data
    const series: { predicate: string; color: string; points: { key: string; value: number }[] }[] = [];
    for (let i = 0; i < sortedPredicates.length; i++) {
        const pred = sortedPredicates[i];
        const color = PREDICATE_COLORS[i % PREDICATE_COLORS.length];
        const hist = histograms[pred];
        const points = allKeys.map(key => ({ key, value: hist[key] || 0 }));
        series.push({ predicate: pred, color, points });
    }

    // Chart dimensions
    const chartWidth = container.clientWidth || 800;
    const chartHeight = 200;
    const marginLeft = 50;
    const marginRight = 16;
    const marginTop = 8;
    const marginBottom = 24;
    const plotWidth = chartWidth - marginLeft - marginRight;
    const plotHeight = chartHeight - marginTop - marginBottom;

    // Find max value across all series
    let maxValue = 0;
    for (const s of series) {
        for (const p of s.points) {
            if (p.value > maxValue) maxValue = p.value;
        }
    }
    if (maxValue === 0) maxValue = 1;

    // X scale: index-based
    const xScale = (i: number) => marginLeft + (i / Math.max(1, allKeys.length - 1)) * plotWidth;
    const yScale = (v: number) => marginTop + plotHeight - (v / maxValue) * plotHeight;

    // Build SVG paths
    const paths = series.map(s => {
        const d = s.points.map((p, i) => {
            const x = xScale(i);
            const y = yScale(p.value);
            return `${i === 0 ? 'M' : 'L'}${x},${y}`;
        }).join(' ');
        return `<path d="${d}" fill="none" stroke="${s.color}" stroke-width="1.5" opacity="0.8" />`;
    }).join('\n');

    // Y-axis labels
    const yTicks = 4;
    const yLabels: string[] = [];
    for (let i = 0; i <= yTicks; i++) {
        const val = (maxValue / yTicks) * i;
        const y = yScale(val);
        const label = val >= 1000 ? `${(val / 1000).toFixed(1)}k` : Math.round(val).toString();
        yLabels.push(`<text x="${marginLeft - 4}" y="${y}" text-anchor="end" dominant-baseline="middle" fill="#64748b" font-size="9">${label}</text>`);
        yLabels.push(`<line x1="${marginLeft}" y1="${y}" x2="${marginLeft + plotWidth}" y2="${y}" stroke="#1e293b" stroke-width="0.5" />`);
    }

    // X-axis labels (show ~8 labels max)
    // Determine if data spans multiple days
    const firstDate = allKeys[0].substring(0, 10);
    const lastDate = allKeys[allKeys.length - 1].substring(0, 10);
    const multiDay = firstDate !== lastDate;

    const xLabelStep = Math.max(1, Math.floor(allKeys.length / 8));
    const xLabels: string[] = [];
    let lastLabelDate = '';
    for (let i = 0; i < allKeys.length; i += xLabelStep) {
        const x = xScale(i);
        const key = allKeys[i];
        let label: string;

        if (multiDay) {
            // Show "MM-DD HH:MM" for multi-day spans
            const date = key.substring(5, 10);
            const time = key.length >= 13 ? key.substring(11) : '';
            if (date !== lastLabelDate) {
                label = time ? `${date} ${time}` : date;
                lastLabelDate = date;
            } else {
                label = time || date;
            }
        } else if (key.length >= 13) {
            label = key.substring(11);
        } else if (key.length === 10) {
            label = key.substring(5);
        } else {
            label = key;
        }
        xLabels.push(`<text x="${x}" y="${chartHeight - 4}" text-anchor="middle" fill="#64748b" font-size="9">${label}</text>`);
    }

    // Legend
    const watcherMap = getWatchersByPredicate();
    const legendItems = series.map(s => {
        const total = predTotals.find(p => p.predicate === s.predicate)?.total || 0;
        const info = watcherMap.get(s.predicate);
        const eyes = info ? (() => { const st = eyeStyle(info); return `<span style="color: ${st.color}; text-shadow: ${st.shadow}; cursor: default;" title="${info.names.join(', ')}">${Watcher.repeat(info.names.length)}</span>`; })() : '';
        return `<span style="display: inline-flex; align-items: center; gap: 4px; margin-right: 12px; font-size: var(--font-size-sm);">
            <span style="width: 8px; height: 8px; border-radius: 50%; background: ${s.color};"></span>
            <span style="color: #e2e8f0;">${s.predicate}${eyes}</span>
            <span style="color: #64748b;">${total.toLocaleString()}</span>
        </span>`;
    }).join('');

    const rangeIndicator = rangeLabel
        ? `<div style="font-size: var(--font-size-xs); color: #475569; text-align: right; padding: 2px 0;">${rangeLabel} \u2014 scroll to zoom, swipe to pan</div>`
        : '';

    container.innerHTML = `
        <div style="padding: 8px 0;">
            ${rangeIndicator}
            <svg viewBox="0 0 ${chartWidth} ${chartHeight}" style="width: 100%; height: ${chartHeight}px;">
                ${yLabels.join('\n')}
                ${xLabels.join('\n')}
                ${paths}
            </svg>
            <div style="padding: 4px 0; display: flex; flex-wrap: wrap;">${legendItems}</div>
        </div>
    `;
}

interface PerfEntry {
    name: string;
    kind: 'op' | 'mutex';
    count: number;
    min: number;
    max: number;
    avg: number;
}

interface PerfData {
    current: PerfEntry[];
    sparklines: Record<string, (number | null)[]>;
    windows: number;
}

function formatMs(ms: number): string {
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(1)}s`;
}

interface LiveStatus {
    write_lock?: { holder: string; held_ms: number };
    wal_bytes?: number;
    db_bytes?: number;
    dilation?: number;
    mem_pct?: number;
    cpu_pct?: number;
}

function formatBytes(bytes: number): string {
    if (bytes < 1024) return `${bytes}B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)}K`;
    if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)}MB`;
    return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)}GB`;
}

function renderLiveStatus(live: LiveStatus): string {
    const lines: string[] = [];

    // Write lock
    const wl = live.write_lock;
    if (wl) {
        const heldSec = wl.held_ms / 1000;
        const color = heldSec > 30 ? '#ef4444' : heldSec > 5 ? '#f59e0b' : '#4ade80';
        lines.push(`<div style="display: flex; align-items: center; gap: 6px; font-size: var(--font-size-sm);">
            <span style="width: 6px; height: 6px; border-radius: 50%; background: ${color};"></span>
            <span style="color: #e2e8f0;">write: <b>${wl.holder}</b> ${formatMs(wl.held_ms)}</span>
        </div>`);
    } else {
        lines.push(`<div style="display: flex; align-items: center; gap: 6px; font-size: var(--font-size-sm);">
            <span style="width: 6px; height: 6px; border-radius: 50%; background: #4ade80;"></span>
            <span style="color: #64748b;">write: idle</span>
        </div>`);
    }

    // Dilation + pressure — also feed into watcher eye color
    if (live.dilation != null) {
        setDilation(live.dilation);
        const d = live.dilation;
        const color = d >= 1.0 ? '#4ade80' : d >= 0.5 ? '#f59e0b' : '#ef4444';
        const mem = live.mem_pct != null ? `${live.mem_pct.toFixed(0)}% mem` : '';
        const cpu = live.cpu_pct != null ? `${live.cpu_pct.toFixed(0)}% cpu` : '';
        const pressure = [mem, cpu].filter(Boolean).join(' · ');
        lines.push(`<div style="font-size: var(--font-size-sm); color: #94a3b8;">
            dilation <span style="color: ${color};">${d.toFixed(2)}x</span>${pressure ? ` · ${pressure}` : ''}
        </div>`);
    }

    // DB + WAL size
    const sizes: string[] = [];
    if (live.db_bytes != null) sizes.push(`db ${formatBytes(live.db_bytes)}`);
    if (live.wal_bytes != null) sizes.push(`wal ${formatBytes(live.wal_bytes)}`);
    if (sizes.length > 0) {
        lines.push(`<div style="font-size: var(--font-size-sm); color: #64748b;">${sizes.join(' · ')}</div>`);
    }

    return lines.join('');
}

function renderPerformanceSection(container: HTMLElement, perf: PerfData | null, live?: LiveStatus): void {
    let liveHTML = '';
    if (live) {
        liveHTML = `<div style="margin-bottom: 8px;">${renderLiveStatus(live)}</div>`;
    }

    if (!perf || !perf.current || perf.current.length === 0) {
        if (liveHTML) {
            container.innerHTML = `<div style="padding: 8px 0; border-bottom: 1px solid var(--border-color, #333);">${liveHTML}</div>`;
        }
        return;
    }

    const maxVal = Math.max(...perf.current.map(e => e.max));
    if (maxVal === 0) return;

    const rows = perf.current.map(entry => {
        const barWidth = 200;
        const minPx = (entry.min / maxVal) * barWidth;
        const maxPx = (entry.max / maxVal) * barWidth;
        const avgPx = (entry.avg / maxVal) * barWidth;
        const spread = entry.max - entry.min;
        const relVariance = entry.avg > 0 ? spread / entry.avg : 0;

        let color = '#4ade80';
        if (relVariance > 2) color = '#ef4444';
        else if (relVariance > 0.5) color = '#f59e0b';

        const isMutex = entry.kind === 'mutex';
        const label = isMutex ? `\u23F3 ${entry.name}` : entry.name;
        const sparkKey = isMutex ? `mutex:${entry.name}` : entry.name;
        const sparkData = perf.sparklines[sparkKey];
        const sparkSvg = sparkData ? renderSparkline(sparkData) : '';

        return `<div style="margin-bottom: 6px;">
            <div style="display: flex; justify-content: space-between; font-size: var(--font-size-sm); color: #e2e8f0; margin-bottom: 2px;">
                <span style="word-break: break-word; overflow-wrap: break-word;">${label} <span style="color: #64748b;">\u00D7${entry.count}</span></span>
                <span style="white-space: nowrap; margin-left: 8px; color: #94a3b8;">${formatMs(entry.avg)}</span>
            </div>
            <div style="position: relative; height: 8px; background: #1e293b; border-radius: 4px; overflow: hidden;">
                <div style="position: absolute; left: ${minPx}px; width: ${Math.max(2, maxPx - minPx)}px; height: 100%; background: ${color}; opacity: 0.3; border-radius: 4px;"></div>
                <div style="position: absolute; left: ${avgPx}px; width: 2px; height: 100%; background: ${color};"></div>
            </div>
            <div style="display: flex; justify-content: space-between; font-size: var(--font-size-xs); color: #475569;">
                <span>${formatMs(entry.min)}</span>
                <span>${formatMs(entry.max)}</span>
            </div>
            ${sparkSvg ? `<div style="margin-top: 2px;">${sparkSvg}</div>` : ''}
        </div>`;
    }).join('');

    container.innerHTML = `
        <div style="padding: 8px 0; border-bottom: 1px solid var(--border-color, #333);">
            ${liveHTML}
            <span class="label" style="font-size: 11px;">Performance (5m windows):</span>
            <div style="margin-top: 4px;">${rows}</div>
        </div>
    `;
}

function renderSparkline(data: (number | null)[]): string {
    const values = data.filter((v): v is number => v != null);
    if (values.length < 2) return '';

    const w = 80;
    const h = 16;
    const max = Math.max(...values);
    if (max === 0) return '';

    const points = data.map((v, i) => {
        if (v == null) return null;
        const x = (i / (data.length - 1)) * w;
        const y = h - (v / max) * (h - 2) - 1;
        return `${x},${y}`;
    }).filter(Boolean);

    if (points.length < 2) return '';

    return `<svg viewBox="0 0 ${w} ${h}" style="width: ${w}px; height: ${h}px;">
        <polyline points="${points.join(' ')}" fill="none" stroke="#64748b" stroke-width="1" />
    </svg>`;
}

function formatAge(timestamp: string | number): string {
    const ms = typeof timestamp === 'string' ? new Date(timestamp).getTime() : timestamp;
    const ago = Date.now() - ms;
    const minutes = Math.floor(ago / 60000);
    if (minutes < 60) return `${minutes}m ago`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    return `${days}d ago`;
}

function renderPredicateDetail(container: HTMLElement, detail: PredicateDetail): void {
    const rows: string[] = [];
    const s = (label: string, value: string) =>
        `<div style="font-size: var(--font-size-sm); color: #94a3b8; padding: 1px 0;"><span style="color: #64748b;">${label}:</span> ${value}</div>`;

    const cap = (items: string[], limit: number) => {
        if (items.length <= limit) return items.join(', ');
        return items.slice(0, limit).join(', ') + ` (+${items.length - limit} more)`;
    };

    if (detail.actors.length > 0) {
        rows.push(s('actors', cap(detail.actors, 5)));
    }
    if (detail.contexts.length > 0) {
        rows.push(s('contexts', cap(detail.contexts, 5)));
    }
    if (detail.oldestEvicted) {
        rows.push(s('oldest evicted data', formatAge(detail.oldestEvicted)));
    }
    rows.push(s('last eviction', formatAge(detail.lastEviction)));

    container.innerHTML = `<div style="padding: 4px 0 4px 12px; border-left: 2px solid #334155; margin: 2px 0 4px 4px; word-break: break-word; overflow-wrap: break-word;">${rows.join('')}</div>`;
}

export function createDbElement(): Element {
    return {
        id: 'database-element',
        title: 'Database',
        symbol: DB,
        opensAs: 'panel' as const,
        renderContent: () => {
            const content = document.createElement('div');
            dbStatsElement = content;
            createSections(content);
            sendMessage({ type: 'get_database_stats' });
            renderDbStats();
            return content;
        },
    };
}
