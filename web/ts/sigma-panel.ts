import { sendMessage } from './client';
import { Sigma, Watcher } from '@generated/sym.js';
import { spawnSigmaAsWindow } from './components/glyph/sigma-glyph';
import { getWatchersByPredicate, refresh as refreshWatcherPredicates, onWatcherPredicatesChanged, eyeStyle } from './watcher-predicates';
import type { Glyph } from '@qntx/glyphs';
import type { Attestation } from './generated/proto/plugin/grpc/protocol/atsstore';

let panelElement: HTMLElement | null = null;
let cachedDistillation: any = null;

export function updateSigmaPanel(stats: any): void {
    if (!stats?.distillation) return;
    cachedDistillation = stats.distillation;
    if (panelElement) renderPanel();
}

function parseJsonField(raw: string): string[] {
    if (!raw) return [];
    try {
        const parsed = JSON.parse(raw);
        return Array.isArray(parsed) ? parsed : [];
    } catch {
        return [];
    }
}

function sigmaToAttestation(sigma: any): Attestation {
    const attrs = typeof sigma.attributes === 'string'
        ? JSON.parse(sigma.attributes)
        : sigma.attributes;
    return {
        id: sigma.id,
        subjects: parseJsonField(sigma.subjects),
        predicates: parseJsonField(sigma.predicates),
        actors: parseJsonField(sigma.actors),
        contexts: parseJsonField(sigma.contexts),
        timestamp: sigma.timestamp ? new Date(sigma.timestamp).getTime() : 0,
        source: sigma.source || 'distill',
        attributes: attrs,
        created_at: sigma.created_at ? new Date(sigma.created_at).getTime() : 0,
        signature: sigma.signature || new Uint8Array(),
        signer_did: sigma.signer_did || '',
    };
}

function formatNum(n: number): string {
    if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M';
    if (n >= 1_000) return (n / 1_000).toFixed(1) + 'k';
    return String(n);
}

function formatAge(iso: string): string {
    if (!iso) return '';
    const ms = Date.now() - new Date(iso).getTime();
    const hours = Math.floor(ms / 3600000);
    if (hours < 1) return '<1h';
    if (hours < 24) return hours + 'h';
    const days = Math.floor(hours / 24);
    return days + 'd';
}

function extractPredicate(sigma: any): string {
    const preds = parseJsonField(sigma.predicates);
    if (preds.length === 0) return '?';
    return preds[0].replace('distill:', '');
}

function getAttrs(sigma: any): any {
    return typeof sigma.attributes === 'string' ? JSON.parse(sigma.attributes) : sigma.attributes;
}

function renderPanel(): void {
    if (!panelElement || !cachedDistillation) return;
    const d = cachedDistillation;

    let html = '';

    // Summary stats row
    const sigmaCount = d.sigmas || 0;
    const preserved = d.preserved_count ? d.preserved_count.toLocaleString() : '0';
    const oldest = d.oldest ? formatAge(d.oldest) : '';
    const newest = d.newest ? formatAge(d.newest) : '';

    html += `<div style="display: flex; flex-wrap: wrap; gap: 12px; padding: 6px 0; border-bottom: 1px solid #3c3836; font-size: 11px;">
        <span style="color: #a89984;">Sigmas: <span style="color: #ebdbb2; font-weight: 600;">${sigmaCount}</span></span>
        <span style="color: #a89984;">Observations: <span style="color: #ebdbb2; font-weight: 600;">${preserved}</span></span>
        ${oldest ? `<span style="color: #a89984;">Span: <span style="color: #ebdbb2;">${oldest} — ${newest}</span></span>` : ''}
    </div>`;

    // Predicate breakdown — horizontal compact
    if (d.predicates && d.predicates.length > 0) {
        const predColors = ['#fe8019', '#fabd2f', '#b8bb26', '#83a598', '#d3869b', '#8ec07c', '#fb4934', '#d65d0e'];
        html += '<div style="display: flex; flex-wrap: wrap; gap: 8px; padding: 6px 0; border-bottom: 1px solid #3c3836; font-size: 10px;">';
        for (let i = 0; i < d.predicates.length; i++) {
            const p = d.predicates[i];
            const color = predColors[i % predColors.length];
            const info = getWatchersByPredicate().get(p.predicate);
            let eyes = '';
            if (info) {
                const s = eyeStyle(info);
                eyes = '<span style="color: ' + s.color + '; text-shadow: ' + s.shadow + '; margin-left: 2px; cursor: default;" title="' + info.names.join(', ') + '">' + Watcher.repeat(info.names.length) + '</span>';
            }
            html += `<span style="display: inline-flex; align-items: center; gap: 3px;">
                <span style="width: 5px; height: 5px; border-radius: 50%; background: ${color};"></span>
                <span style="color: #bdae93;">${p.predicate}${eyes}</span>
                <span style="color: #7c6f64;">${p.count}</span>
            </span>`;
        }
        html += '</div>';
    }

    // Top sigmas — table layout with timeline
    const topSigmas: any[] = d.top_sigmas || [];
    if (topSigmas.length > 0) {
        // Compute global time range across all sigmas for the timeline axis
        let globalMin = Infinity;
        let globalMax = -Infinity;
        const now = Date.now();
        for (const sigma of topSigmas) {
            const attrs = getAttrs(sigma);
            if (attrs?._first_seen) {
                const t = new Date(attrs._first_seen).getTime();
                if (t < globalMin) globalMin = t;
            }
            if (attrs?._last_seen) {
                const t = new Date(attrs._last_seen).getTime();
                if (t > globalMax) globalMax = t;
            }
        }
        // Extend to now if last_seen is close to present
        if (now - globalMax < 86400000) globalMax = now;
        const globalSpan = globalMax - globalMin || 1;

        // Generate day-level gridlines
        const dayMs = 86400000;
        const firstDay = new Date(globalMin);
        firstDay.setUTCHours(0, 0, 0, 0);
        let gridDay = firstDay.getTime();
        // Advance to next day boundary if globalMin is partway through a day
        if (gridDay < globalMin) gridDay += dayMs;

        const gridlines: { pct: number; label: string }[] = [];
        while (gridDay < globalMax) {
            const pct = ((gridDay - globalMin) / globalSpan) * 100;
            if (pct > 2 && pct < 98) {
                const d2 = new Date(gridDay);
                gridlines.push({ pct, label: d2.toLocaleDateString('en', { month: 'short', day: 'numeric' }) });
            }
            gridDay += dayMs;
        }

        // Format axis labels
        const axisStart = new Date(globalMin).toLocaleDateString('en', { month: 'short', day: 'numeric' });
        const axisEnd = globalMax === now ? 'now' : new Date(globalMax).toLocaleDateString('en', { month: 'short', day: 'numeric' });

        // Build gridline CSS for the timeline column
        const gridlineOverlay = gridlines.map(g =>
            `<div style="position: absolute; left: ${g.pct}%; top: 0; bottom: 0; width: 1px; background: #504945;" title="${g.label}"></div>`
        ).join('');

        // Table header with gridline labels
        const gridLabels = gridlines.map(g =>
            `<span style="position: absolute; left: ${g.pct}%; transform: translateX(-50%); white-space: nowrap;">${g.label}</span>`
        ).join('');

        // Table header
        html += `<table style="width: 100%; border-collapse: collapse; font-size: 10px; margin-top: 4px;">
        <thead>
            <tr style="color: #7c6f64; font-size: 8px; text-transform: uppercase; letter-spacing: 0.5px;">
                <th style="text-align: right; padding: 1px 4px; white-space: nowrap;">Obs</th>
                <th style="text-align: left; padding: 1px 4px;">Predicate</th>
                <th style="text-align: right; padding: 1px 4px; white-space: nowrap;">Subj</th>
                <th style="text-align: right; padding: 1px 4px; white-space: nowrap;">Bat</th>
                <th style="text-align: left; padding: 1px 4px; min-width: 40%; position: relative;">
                    <span style="float: left;">${axisStart}</span>
                    ${gridLabels}
                    <span style="float: right;">${axisEnd}</span>
                </th>
            </tr>
        </thead>
        <tbody>`;

        for (const sigma of topSigmas) {
            const attrs = getAttrs(sigma);
            const total = attrs?._total || attrs?._count || 0;
            const count = attrs?._count || 0;
            const predicate = extractPredicate(sigma);

            // Subjects count
            const subjCount = attrs?._subjects_count || 0;

            // Timeline bar position
            const fs = attrs?._first_seen ? new Date(attrs._first_seen).getTime() : globalMin;
            const ls = attrs?._last_seen ? new Date(attrs._last_seen).getTime() : globalMax;
            const left = ((fs - globalMin) / globalSpan) * 100;
            const width = Math.max(0.5, ((ls - fs) / globalSpan) * 100);

            // Gruvbox palette — brighter for heavier sigmas
            const barColor = total > 100000 ? '#fe8019' : total > 10000 ? '#d65d0e' : '#af3a03';

            html += `<tr class="sigma-panel-row" data-sigma-id="${sigma.id}" style="cursor: pointer; border-bottom: 1px solid #3c3836; line-height: 1.1;">
                <td style="padding: 1px 4px; text-align: right; color: #ebdbb2; font-weight: 600; white-space: nowrap; font-variant-numeric: tabular-nums;">${formatNum(total)}</td>
                <td style="padding: 1px 4px; color: #bdae93;">${predicate}</td>
                <td style="padding: 1px 4px; text-align: right; color: #7c6f64; white-space: nowrap; font-variant-numeric: tabular-nums;">${subjCount > 0 ? formatNum(subjCount) : ''}</td>
                <td style="padding: 1px 4px; text-align: right; color: #7c6f64; white-space: nowrap; font-variant-numeric: tabular-nums;">${count}</td>
                <td style="padding: 1px 4px;">
                    <div style="position: relative; width: 100%; height: 6px; background: #3c3836; border-radius: 3px;">
                        ${gridlineOverlay}
                        <div style="position: absolute; left: ${left}%; width: ${width}%; height: 100%; background: ${barColor}; border-radius: 3px; min-width: 2px; z-index: 1;"></div>
                    </div>
                </td>
            </tr>`;
        }

        html += '</tbody></table>';
    } else {
        html += '<div style="padding: 16px 0; text-align: center; color: #7c6f64; font-size: 11px;">No sigmas with >= 100 observations yet</div>';
    }

    panelElement.innerHTML = html;

    // Wire click handlers
    panelElement.querySelectorAll('.sigma-panel-row').forEach(row => {
        row.addEventListener('click', () => {
            const sigmaId = (row as HTMLElement).dataset.sigmaId;
            const sigma = topSigmas.find((s: any) => s.id === sigmaId);
            if (sigma) {
                spawnSigmaAsWindow(sigmaToAttestation(sigma));
            }
        });
    });
}

export function createSigmaPanel(): Glyph {
    return {
        id: 'sigma-panel',
        title: `${Sigma} Sigma`,
        manifestationType: 'panel' as const,
        renderContent: () => {
            const content = document.createElement('div');
            panelElement = content;
            sendMessage({ type: 'get_database_stats' });
            refreshWatcherPredicates();
            onWatcherPredicatesChanged(() => { if (panelElement) renderPanel(); });
            if (cachedDistillation) renderPanel();
            return content;
        },
    };
}
