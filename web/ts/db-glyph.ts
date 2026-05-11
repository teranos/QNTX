import { sendMessage } from './websocket';
import { DB } from '@generated/sym.js';
import { seedEvictions, recordEviction as recordEvictionEvent, getEvictionSummary, hasEvictions, renderEvictionChart, getPredicateBreakdown, type PredicateDetail } from './eviction-chart';
import type { Glyph } from '@qntx/glyphs';

let dbStatsElement: HTMLElement | null = null;
let dbStats: any = null;

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

function renderDbStats(): void {
    if (!dbStatsElement) return;

    if (!dbStats || !dbStats.total_attestations) {
        const msg = dbStats?.error ?? 'Loading database statistics...';
        dbStatsElement.innerHTML = `<div class="glyph-loading">${msg}</div>`;
        return;
    }

    const storageBackend = dbStats.storage_optimized
        ? `rust (optimized) v${dbStats.storage_version}`
        : 'go (fallback)';

    // Build rich fields / types section
    let typesSection = '';
    const richFields = dbStats.rich_fields;
    if (richFields && richFields.length > 0) {
        const isEnhanced = typeof richFields[0] === 'object' && 'field' in richFields[0];
        const fieldItems = isEnhanced
            ? richFields
                .sort((a: any, b: any) => b.count - a.count)
                .map((f: any) => `<span class="glyph-type-link" data-type="${f.field}" style="cursor: pointer; text-decoration: underline; margin-right: 8px;">${f.field} (${f.count})</span>`)
                .join('')
            : richFields.sort().map((f: string) => `<span class="glyph-type-link" data-type="${f}" style="cursor: pointer; text-decoration: underline; margin-right: 8px;">${f}</span>`).join('');

        typesSection = `
            <div class="glyph-row" style="margin-top: 8px; border-top: 1px solid var(--border-color, #333); padding-top: 8px;">
                <span class="glyph-label">Types (${richFields.length}):</span>
                <span class="glyph-value" style="display: flex; flex-wrap: wrap; gap: 4px;">${fieldItems}</span>
            </div>
        `;
    }

    // Build eviction section — bar chart aggregated by hour + predicate breakdown
    let evictionSection = '';
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

        evictionSection = `
            <div class="glyph-row" style="margin-top: 8px; border-top: 1px solid var(--border-color, #333); padding-top: 8px;">
                <span class="glyph-label">Evictions:</span>
                <span class="glyph-value">${summary.count} events, ${summary.totalEvicted.toLocaleString()} attestations evicted</span>
            </div>
            <div class="eviction-chart-container"></div>
            ${predicateRows}
        `;
    }

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
            ${typesSection}
            ${evictionSection}
            <div class="perf-section-container"></div>
        </div>
    `;

    // Wire up type links to open type definition window
    dbStatsElement.querySelectorAll('.glyph-type-link').forEach(el => {
        el.addEventListener('click', () => {
            const typeName = (el as HTMLElement).dataset.type;
            if (typeName) {
                import('./type-definition-window.js').then(({ openTypeDefinition }) => {
                    openTypeDefinition(typeName);
                });
            }
        });
    });

    // Render eviction bar chart
    const chartContainer = dbStatsElement.querySelector('.eviction-chart-container');
    if (chartContainer && hasEvictions()) {
        renderEvictionChart(chartContainer as HTMLElement);
    }

    // Wire predicate drill-down
    const breakdown = getPredicateBreakdown();
    dbStatsElement.querySelectorAll('.eviction-pred-row').forEach(el => {
        el.addEventListener('click', () => {
            const idx = parseInt((el as HTMLElement).dataset.predIdx ?? '', 10);
            const detail = dbStatsElement!.querySelector(`[data-pred-detail="${idx}"]`) as HTMLElement;
            if (!detail || isNaN(idx)) return;
            if (detail.style.display === 'none') {
                detail.style.display = 'block';
                renderPredicateDetail(detail, breakdown[idx]);
            } else {
                detail.style.display = 'none';
            }
        });
    });

    // Render performance section
    const perfContainer = dbStatsElement.querySelector('.perf-section-container');
    if (perfContainer && dbStats.performance) {
        renderPerformanceSection(perfContainer as HTMLElement, dbStats.performance);
    }
}

interface PerfEntry {
    name: string;
    kind: 'op' | 'mutex';
    count: number;
    min: number; // ms
    max: number; // ms
    avg: number; // ms
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

function renderPerformanceSection(container: HTMLElement, perf: PerfData): void {
    if (!perf.current || perf.current.length === 0) return;

    const maxVal = Math.max(...perf.current.map(e => e.max));
    if (maxVal === 0) return;

    const rows = perf.current.map(entry => {
        const barWidth = 200;
        const minPx = (entry.min / maxVal) * barWidth;
        const maxPx = (entry.max / maxVal) * barWidth;
        const avgPx = (entry.avg / maxVal) * barWidth;
        const spread = entry.max - entry.min;
        const relVariance = entry.avg > 0 ? spread / entry.avg : 0;

        // Color by severity: green < 0.5 variance, amber 0.5-2, red > 2
        let color = '#4ade80'; // green
        if (relVariance > 2) color = '#ef4444'; // red
        else if (relVariance > 0.5) color = '#f59e0b'; // amber

        const isMutex = entry.kind === 'mutex';
        const label = isMutex ? `⏳ ${entry.name}` : entry.name;
        const sparkKey = isMutex ? `mutex:${entry.name}` : entry.name;
        const sparkData = perf.sparklines[sparkKey];
        const sparkSvg = sparkData ? renderSparkline(sparkData) : '';

        return `<div style="margin-bottom: 6px;">
            <div style="display: flex; justify-content: space-between; font-size: 10px; color: #e2e8f0; margin-bottom: 2px;">
                <span style="word-break: break-word; overflow-wrap: break-word;">${label} <span style="color: #64748b;">×${entry.count}</span></span>
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
        <div style="margin-top: 8px; border-top: 1px solid var(--border-color, #333); padding-top: 8px;">
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
    };
}
