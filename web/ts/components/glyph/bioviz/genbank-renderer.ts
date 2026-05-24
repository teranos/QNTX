/**
 * GenBank format renderer — linear cassette map.
 *
 * Parses GenBank flat-file text, extracts LOCUS header and FEATURES,
 * renders an SVG linear map with colored feature blocks and labels.
 */

import { preventDrag } from '@qntx/glyphs';
import { AZURE_KEYWORD, AZURE_VALUE } from '../attestation-attrs';

interface Feature {
    label: string;
    start: number;
    end: number;
}

interface GenbankData {
    locus: string;
    length: number;
    definition: string;
    features: Feature[];
}

// Muted palette for feature blocks — distinct from each other, fits dark theme
const FEATURE_COLORS = [
    '#5a8faa', // blue
    '#6aaa7a', // green
    '#aa8a5a', // amber
    '#aa5a6a', // rose
    '#8a6aaa', // purple
    '#5aaaa0', // teal
    '#aa7a5a', // orange
    '#7a8aaa', // steel
    '#8aaa5a', // lime
    '#aa5a8a', // magenta
];

/**
 * Detect whether a string is GenBank format.
 */
export function isGenbankData(value: string): boolean {
    return value.startsWith('LOCUS') && value.indexOf('\nFEATURES') !== -1;
}

function parseGenbank(text: string): GenbankData {
    const lines = text.split('\n');

    // LOCUS line: "LOCUS       Q9UEF7           3470 bp    DNA ..."
    let locus = '';
    let length = 0;
    let definition = '';
    const features: Feature[] = [];

    let i = 0;
    // Parse LOCUS
    if (lines[0].startsWith('LOCUS')) {
        const parts = lines[0].split(/\s+/);
        locus = parts[1] || '';
        for (let p = 2; p < parts.length; p++) {
            if (parts[p] === 'bp' && p > 0) {
                length = parseInt(parts[p - 1], 10) || 0;
                break;
            }
        }
        i = 1;
    }

    // Parse DEFINITION
    for (; i < lines.length; i++) {
        if (lines[i].startsWith('DEFINITION')) {
            definition = lines[i].slice(10).trim();
            i++;
            break;
        }
    }

    // Parse FEATURES
    let inFeatures = false;
    let currentStart = 0;
    let currentEnd = 0;
    let currentLabel = '';

    for (; i < lines.length; i++) {
        const line = lines[i];
        if (line.startsWith('FEATURES')) {
            inFeatures = true;
            continue;
        }
        if (line.startsWith('ORIGIN') || line.startsWith('//')) break;
        if (!inFeatures) continue;

        const trimmed = line.trim();

        // Feature line: "misc_feature    1..42"
        if (trimmed.startsWith('misc_feature') || trimmed.startsWith('gene') ||
            trimmed.startsWith('CDS') || trimmed.startsWith('promoter') ||
            trimmed.startsWith('rep_origin') || trimmed.startsWith('regulatory') ||
            trimmed.startsWith('polyA_signal') || trimmed.startsWith('sig_peptide') ||
            trimmed.startsWith('source')) {
            // Save previous feature
            if (currentLabel && currentEnd > 0) {
                features.push({ label: currentLabel, start: currentStart, end: currentEnd });
            }
            // Parse range
            const rangePart = trimmed.split(/\s+/)[1] || '';
            const dotIdx = rangePart.indexOf('..');
            if (dotIdx !== -1) {
                currentStart = parseInt(rangePart.slice(0, dotIdx), 10) || 0;
                currentEnd = parseInt(rangePart.slice(dotIdx + 2), 10) || 0;
            }
            currentLabel = '';
            continue;
        }

        // Label qualifier: /label="attB"
        if (trimmed.startsWith('/label=')) {
            currentLabel = trimmed.slice(8, -1); // strip /label=" and trailing "
        }
    }
    // Save last feature
    if (currentLabel && currentEnd > 0) {
        features.push({ label: currentLabel, start: currentStart, end: currentEnd });
    }

    return { locus, length, definition, features };
}

/**
 * Build a linear cassette map SVG for GenBank data.
 */
export function buildGenbankViewer(genbankText: string): HTMLElement {
    const data = parseGenbank(genbankText);
    const wrapper = document.createElement('div');
    wrapper.style.width = '100%';
    wrapper.style.marginBottom = '8px';
    preventDrag(wrapper);

    // Header: LOCUS — length bp
    const header = document.createElement('div');
    header.style.fontSize = '11px';
    header.style.fontFamily = 'monospace';
    header.style.marginBottom = '4px';
    header.style.color = AZURE_VALUE;
    header.textContent = `${data.definition || data.locus} — ${data.length} bp`;
    wrapper.appendChild(header);

    if (data.features.length === 0 || data.length === 0) {
        return wrapper;
    }

    // SVG linear map
    const padLeft = 8;
    const padRight = 8;
    const barY = 24;
    const barH = 16;
    const svgW = 500;
    const trackW = svgW - padLeft - padRight;

    // Calculate label rows to avoid overlap
    const labelRows = assignLabelRows(data.features, data.length, trackW, padLeft);
    const maxRow = labelRows.reduce((m, r) => (r > m ? r : m), 0);
    const labelAreaH = (maxRow + 1) * 16;
    const svgH = barY + barH + 6 + labelAreaH + 4;

    const ns = 'http://www.w3.org/2000/svg';
    const svg = document.createElementNS(ns, 'svg');
    svg.setAttribute('viewBox', `0 0 ${svgW} ${svgH}`);
    svg.style.width = '100%';
    svg.style.maxHeight = `${svgH}px`;
    svg.style.display = 'block';

    // Background bar (full length)
    const bg = document.createElementNS(ns, 'rect');
    bg.setAttribute('x', String(padLeft));
    bg.setAttribute('y', String(barY));
    bg.setAttribute('width', String(trackW));
    bg.setAttribute('height', String(barH));
    bg.setAttribute('rx', '3');
    bg.setAttribute('fill', 'rgba(255,255,255,0.04)');
    svg.appendChild(bg);

    // Feature blocks + labels
    for (let fi = 0; fi < data.features.length; fi++) {
        const f = data.features[fi];
        const color = FEATURE_COLORS[fi % FEATURE_COLORS.length];
        const x = padLeft + ((f.start - 1) / data.length) * trackW;
        const w = Math.max(2, ((f.end - f.start + 1) / data.length) * trackW);

        // Feature block
        const rect = document.createElementNS(ns, 'rect');
        rect.setAttribute('x', String(x));
        rect.setAttribute('y', String(barY));
        rect.setAttribute('width', String(w));
        rect.setAttribute('height', String(barH));
        rect.setAttribute('rx', '2');
        rect.setAttribute('fill', color);
        rect.setAttribute('opacity', '0.8');
        svg.appendChild(rect);

        // Label below bar
        const labelY = barY + barH + 6 + labelRows[fi] * 16 + 10;
        const midX = x + w / 2;

        // Tick line from bar to label
        const tick = document.createElementNS(ns, 'line');
        tick.setAttribute('x1', String(midX));
        tick.setAttribute('y1', String(barY + barH));
        tick.setAttribute('x2', String(midX));
        tick.setAttribute('y2', String(labelY - 10));
        tick.setAttribute('stroke', color);
        tick.setAttribute('stroke-width', '0.7');
        tick.setAttribute('opacity', '0.5');
        svg.appendChild(tick);

        const text = document.createElementNS(ns, 'text');
        text.setAttribute('x', String(midX));
        text.setAttribute('y', String(labelY));
        text.setAttribute('text-anchor', 'middle');
        text.setAttribute('font-family', 'monospace');
        text.setAttribute('font-size', '9');
        text.setAttribute('fill', color);
        text.textContent = f.label;
        svg.appendChild(text);
    }

    // Scale ticks (start, end)
    for (const [pos, anchor] of [[1, 'start'], [data.length, 'end']] as [number, string][]) {
        const x = padLeft + ((pos - 1) / data.length) * trackW;
        const tick = document.createElementNS(ns, 'text');
        tick.setAttribute('x', String(anchor === 'start' ? x : x + trackW / data.length));
        tick.setAttribute('y', String(barY - 4));
        tick.setAttribute('text-anchor', anchor);
        tick.setAttribute('font-family', 'monospace');
        tick.setAttribute('font-size', '8');
        tick.setAttribute('fill', AZURE_KEYWORD);
        tick.textContent = String(pos);
        svg.appendChild(tick);
    }

    wrapper.appendChild(svg);
    return wrapper;
}

/**
 * Assign label rows to avoid horizontal overlap.
 * Returns array of row indices (0-based) parallel to features.
 */
function assignLabelRows(features: Feature[], seqLength: number, trackW: number, padLeft: number): number[] {
    const charW = 5.4; // approximate monospace char width at font-size 9
    const rows: number[] = new Array(features.length).fill(0);
    // Track occupied ranges per row: [left, right] in SVG x coordinates
    const occupied: [number, number][][] = [[]];

    for (let i = 0; i < features.length; i++) {
        const f = features[i];
        const x = padLeft + ((f.start - 1) / seqLength) * trackW;
        const w = Math.max(2, ((f.end - f.start + 1) / seqLength) * trackW);
        const midX = x + w / 2;
        const labelHalfW = (f.label.length * charW) / 2 + 4;
        const left = midX - labelHalfW;
        const right = midX + labelHalfW;

        let placed = false;
        for (let row = 0; row < occupied.length; row++) {
            const overlaps = occupied[row].some(([l, r]) => left < r && right > l);
            if (!overlaps) {
                rows[i] = row;
                occupied[row].push([left, right]);
                placed = true;
                break;
            }
        }
        if (!placed) {
            rows[i] = occupied.length;
            occupied.push([[left, right]]);
        }
    }

    return rows;
}
