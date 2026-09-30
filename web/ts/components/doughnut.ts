/**
 * Doughnut: how the answers to one question divide, drawn as Umami draws its
 * UTM report (PieChart type="doughnut" in umami src/app/(main)/websites/
 * [websiteId]/(reports)/utm/UTM.tsx, colors CHART_COLORS in src/lib/constants.ts).
 *
 * "We take a different route from Umami because we believe count summations
 * don't mean a lot." No list beside the ring and no count on it: pointing at a
 * segment says its name and value, and a longer hover reveals the whole legend,
 * said in tooltip form: an element of @teranos/elements, a new one every time.
 */

import { tooltipFrom } from '@teranos/elements';
import { SAID_TIMING, saidId } from './said';

const SVG = 'http://www.w3.org/2000/svg';

/** Umami's CHART_COLORS, in its order. */
export const CHART_COLORS = [
    '#2680eb',
    '#9256d9',
    '#44b556',
    '#e68619',
    '#e34850',
    '#f7bd12',
    '#01bad7',
    '#6734bc',
    '#89c541',
    '#ffc301',
    '#ec1562',
    '#ffec16',
];

/** One answer and how many said it. */
export interface Slice {
    name: string;
    value: number;
}

/** One legend entry: the answer, how many, and its color. */
type Entry = [string, number, string];

/**
 * A ring of slices in the order given, each in the next of Umami's colors.
 * Null when nothing was said, so the caller says so in words. Named, the
 * window its legend may become carries the name.
 */
export function renderDoughnut(slices: Slice[], size = 96, name = ''): SVGSVGElement | null {
    const said = slices.filter((s) => s.value > 0);
    const total = said.reduce((sum, s) => sum + s.value, 0);
    if (total === 0) return null;

    // Chart.js draws a doughnut with half its radius cut out, which is Umami's.
    const outer = size / 2;
    const width = outer / 2;
    const r = outer - width / 2;
    const around = 2 * Math.PI * r;

    const ring = document.createElementNS(SVG, 'svg');
    ring.setAttribute('class', 'doughnut');
    ring.setAttribute('viewBox', `0 0 ${size} ${size}`);
    ring.setAttribute('width', String(size));
    ring.setAttribute('height', String(size));

    const legend: Entry[] = [];
    let offset = 0;
    said.forEach((slice, i) => {
        const color = CHART_COLORS[i % CHART_COLORS.length];
        legend.push([slice.name, slice.value, color]);
        const length = (slice.value / total) * around;

        const segment = document.createElementNS(SVG, 'circle');
        segment.setAttribute('class', 'doughnut-segment');
        segment.setAttribute('cx', String(outer));
        segment.setAttribute('cy', String(outer));
        segment.setAttribute('r', String(r));
        segment.setAttribute('fill', 'none');
        segment.setAttribute('stroke', color);
        segment.setAttribute('stroke-width', String(width));
        segment.setAttribute('stroke-dasharray', `${length} ${around - length}`);
        segment.setAttribute('stroke-dashoffset', String(-offset));
        // From twelve o'clock, clockwise, as Chart.js starts.
        segment.setAttribute('transform', `rotate(-90 ${outer} ${outer})`);
        const entry = `${slice.name} · ${slice.value}`;
        tooltipFrom(segment, () => ({
            id: saidId('ring'),
            title: name || entry,
            renderContent: () => wholeLegend(legend),
        }), SAID_TIMING, () => entry);
        ring.appendChild(segment);
        offset += length;
    });

    return ring;
}

/** The whole legend of a ring, for a longer hover: every answer, its color and how many. */
export function wholeLegend(legend: Entry[]): HTMLElement {
    const list = document.createElement('div');
    list.className = 'doughnut-legend';
    for (const [name, value, color] of legend) {
        const entry = document.createElement('div');
        entry.style.display = 'flex';
        entry.style.alignItems = 'center';
        entry.style.gap = '6px';

        const swatch = document.createElement('span');
        swatch.style.width = '8px';
        swatch.style.height = '8px';
        swatch.style.borderRadius = '50%';
        swatch.style.flexShrink = '0';
        swatch.style.background = color;

        const said = document.createElement('span');
        said.textContent = `${name} · ${value}`;

        entry.append(swatch, said);
        list.appendChild(entry);
    }
    return list;
}
