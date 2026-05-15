import { sendMessage } from './websocket';
import { DB } from '@generated/sym.js';
import { seedEvictions, recordEviction as recordEvictionEvent, getEvictionSummary, hasEvictions, renderEvictionChart, getPredicateBreakdown, type PredicateDetail } from './eviction-chart';
import type { Glyph } from '@qntx/glyphs';

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
    if (stats.error && dbStatsElement) {
        // Cache not ready yet — retry in 2s
        setTimeout(() => sendMessage({ type: 'get_database_stats' }), 2000);
        return;
    }
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

function renderDbStats(): void {
    if (!dbStatsElement || !sectionChart || !sectionOverview || !sectionPredicates || !sectionEvictions || !sectionPerformance) return;

    if (!dbStats || !dbStats.total_attestations) {
        const msg = dbStats?.error ?? 'Loading database statistics...';
        sectionChart.innerHTML = `<div class="glyph-loading">${msg}</div>`;
        return;
    }

    // -- Chart: multi-predicate timeseries with range selector --
    renderChartWithControls(sectionChart, dbStats.predicate_histograms);

    // -- Overview: compact stats row --
    const storageBackend = dbStats.storage_optimized
        ? `rust (optimized) v${dbStats.storage_version}`
        : 'go (fallback)';

    sectionOverview.innerHTML = `
        <div style="display: flex; flex-wrap: wrap; gap: 16px; padding: 8px 0; border-bottom: 1px solid var(--border-color, #333); font-size: 11px;">
            <span><span class="glyph-label">Path:</span> <span class="glyph-value">${dbStats.path}</span></span>
            <span><span class="glyph-label">Backend:</span> <span class="glyph-value">${storageBackend}</span></span>
            <span><span class="glyph-label">Attestations:</span> <span class="glyph-value">${dbStats.total_attestations.toLocaleString()}</span></span>
            <span><span class="glyph-label">Actors:</span> <span class="glyph-value">${dbStats.unique_actors.toLocaleString()}</span></span>
            <span><span class="glyph-label">Subjects:</span> <span class="glyph-value">${dbStats.unique_subjects.toLocaleString()}</span></span>
            <span><span class="glyph-label">Contexts:</span> <span class="glyph-value">${dbStats.unique_contexts.toLocaleString()}</span></span>
        </div>
    `;

    // -- Predicates: types + distillation --
    let predicatesHTML = '';

    // Rich fields / types
    const richFields = dbStats.rich_fields;
    if (richFields && richFields.length > 0) {
        const isEnhanced = typeof richFields[0] === 'object' && 'field' in richFields[0];
        const fieldItems = isEnhanced
            ? richFields
                .sort((a: any, b: any) => b.count - a.count)
                .map((f: any) => `<span class="glyph-type-link" data-type="${f.field}" style="cursor: pointer; text-decoration: underline; margin-right: 8px;">${f.field} (${f.count})</span>`)
                .join('')
            : richFields.sort().map((f: string) => `<span class="glyph-type-link" data-type="${f}" style="cursor: pointer; text-decoration: underline; margin-right: 8px;">${f}</span>`).join('');

        predicatesHTML += `
            <div style="margin-bottom: 8px;">
                <span class="glyph-label">Types (${richFields.length}):</span>
                <span class="glyph-value" style="display: flex; flex-wrap: wrap; gap: 4px;">${fieldItems}</span>
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
                <span class="glyph-label">Distillation:</span>
                <span class="glyph-value">${d.sigmas} sigmas, ${preserved} original preserved</span>
                ${timeRange ? `<span style="color: #64748b; margin-left: 8px;">${timeRange}</span>` : ''}
            </div>
        `;

        // Predicate list with color indicators matching chart
        if (d.predicates && d.predicates.length > 0) {
            const predRows = d.predicates.map((p: { predicate: string; count: number }, i: number) => {
                const color = PREDICATE_COLORS[i % PREDICATE_COLORS.length];
                return `<div style="display: flex; align-items: center; gap: 6px; font-size: 11px; padding: 2px 0;">
                    <span style="width: 8px; height: 8px; border-radius: 50%; background: ${color}; flex-shrink: 0;"></span>
                    <span style="color: #e2e8f0; word-break: break-word; overflow-wrap: break-word; flex: 1;">${p.predicate}</span>
                    <span style="color: #94a3b8; white-space: nowrap;">${p.count}</span>
                </div>`;
            }).join('');
            predicatesHTML += `<div style="margin-top: 4px;">${predRows}</div>`;
        }
    }

    sectionPredicates.innerHTML = predicatesHTML ? `<div style="padding: 8px 0; border-bottom: 1px solid var(--border-color, #333);">${predicatesHTML}</div>` : '';

    // Wire type links
    sectionPredicates.querySelectorAll('.glyph-type-link').forEach(el => {
        el.addEventListener('click', () => {
            const typeName = (el as HTMLElement).dataset.type;
            if (typeName) {
                import('./type-definition-window.js').then(({ openTypeDefinition }) => {
                    openTypeDefinition(typeName);
                });
            }
        });
    });

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
                    <span class="glyph-label" style="font-size: 11px;">Evicted predicates:</span>
                    ${items}
                </div>
            `;
        }

        sectionEvictions.innerHTML = `
            <div style="padding: 8px 0; border-bottom: 1px solid var(--border-color, #333);">
                <div style="margin-bottom: 4px;">
                    <span class="glyph-label">Evictions:</span>
                    <span class="glyph-value">${summary.count} events, ${summary.totalEvicted.toLocaleString()} attestations evicted</span>
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
    const legendItems = series.map(s => {
        const total = predTotals.find(p => p.predicate === s.predicate)?.total || 0;
        return `<span style="display: inline-flex; align-items: center; gap: 4px; margin-right: 12px; font-size: 10px;">
            <span style="width: 8px; height: 8px; border-radius: 50%; background: ${s.color};"></span>
            <span style="color: #e2e8f0;">${s.predicate}</span>
            <span style="color: #64748b;">${total.toLocaleString()}</span>
        </span>`;
    }).join('');

    const rangeIndicator = rangeLabel
        ? `<div style="font-size: 9px; color: #475569; text-align: right; padding: 2px 0;">${rangeLabel} \u2014 scroll to zoom, swipe to pan</div>`
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
        lines.push(`<div style="display: flex; align-items: center; gap: 6px; font-size: 10px;">
            <span style="width: 6px; height: 6px; border-radius: 50%; background: ${color};"></span>
            <span style="color: #e2e8f0;">write: <b>${wl.holder}</b> ${formatMs(wl.held_ms)}</span>
        </div>`);
    } else {
        lines.push(`<div style="display: flex; align-items: center; gap: 6px; font-size: 10px;">
            <span style="width: 6px; height: 6px; border-radius: 50%; background: #4ade80;"></span>
            <span style="color: #64748b;">write: idle</span>
        </div>`);
    }

    // Dilation + pressure
    if (live.dilation != null) {
        const d = live.dilation;
        const color = d >= 1.0 ? '#4ade80' : d >= 0.5 ? '#f59e0b' : '#ef4444';
        const mem = live.mem_pct != null ? `${live.mem_pct.toFixed(0)}% mem` : '';
        const cpu = live.cpu_pct != null ? `${live.cpu_pct.toFixed(0)}% cpu` : '';
        const pressure = [mem, cpu].filter(Boolean).join(' · ');
        lines.push(`<div style="font-size: 10px; color: #94a3b8;">
            dilation <span style="color: ${color};">${d.toFixed(2)}x</span>${pressure ? ` · ${pressure}` : ''}
        </div>`);
    }

    // DB + WAL size
    const sizes: string[] = [];
    if (live.db_bytes != null) sizes.push(`db ${formatBytes(live.db_bytes)}`);
    if (live.wal_bytes != null) sizes.push(`wal ${formatBytes(live.wal_bytes)}`);
    if (sizes.length > 0) {
        lines.push(`<div style="font-size: 10px; color: #64748b;">${sizes.join(' · ')}</div>`);
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
            <div style="display: flex; justify-content: space-between; font-size: 10px; color: #e2e8f0; margin-bottom: 2px;">
                <span style="word-break: break-word; overflow-wrap: break-word;">${label} <span style="color: #64748b;">\u00D7${entry.count}</span></span>
                <span style="white-space: nowrap; margin-left: 8px; color: #94a3b8;">${formatMs(entry.avg)}</span>
            </div>
            <div style="position: relative; height: 8px; background: #1e293b; border-radius: 4px; overflow: hidden;">
                <div style="position: absolute; left: ${minPx}px; width: ${Math.max(2, maxPx - minPx)}px; height: 100%; background: ${color}; opacity: 0.3; border-radius: 4px;"></div>
                <div style="position: absolute; left: ${avgPx}px; width: 2px; height: 100%; background: ${color};"></div>
            </div>
            <div style="display: flex; justify-content: space-between; font-size: 9px; color: #475569;">
                <span>${formatMs(entry.min)}</span>
                <span>${formatMs(entry.max)}</span>
            </div>
            ${sparkSvg ? `<div style="margin-top: 2px;">${sparkSvg}</div>` : ''}
        </div>`;
    }).join('');

    container.innerHTML = `
        <div style="padding: 8px 0; border-bottom: 1px solid var(--border-color, #333);">
            ${liveHTML}
            <span class="glyph-label" style="font-size: 11px;">Performance (5m windows):</span>
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
        `<div style="font-size: 10px; color: #94a3b8; padding: 1px 0;"><span style="color: #64748b;">${label}:</span> ${value}</div>`;

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

export function createDbGlyph(): Glyph {
    return {
        id: 'database-glyph',
        title: `${DB} Database`,
        manifestationType: 'panel' as const,
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
