/**
 * RNA/DNA secondary structure renderer — arc diagram.
 *
 * Takes a sequence and dot-bracket structure, renders an SVG arc diagram:
 * colored bases along the bottom, semicircular arcs connecting paired positions.
 * Arc height proportional to pair distance. Color by nesting depth.
 */

import { preventDrag } from '@qntx/glyphs';

const BASE_COLORS: Record<string, string> = {
    A: '#66bb6a',
    T: '#ef5350',
    U: '#ef5350',
    G: '#ffca28',
    C: '#42a5f5',
};

const DEPTH_COLORS = [
    '#5a8faa', // 0 — muted blue
    '#6aaa7a', // 1 — muted green
    '#aa8a5a', // 2 — muted amber
    '#aa5a6a', // 3 — muted rose
    '#8a6aaa', // 4 — muted purple
    '#5aaaa0', // 5 — muted teal
];

interface BasePair {
    i: number;
    j: number;
    depth: number;
}

function parseDotBracket(structure: string): BasePair[] {
    const pairs: BasePair[] = [];
    const stack: number[] = [];
    const depths: number[] = new Array(structure.length).fill(0);

    for (let k = 0; k < structure.length; k++) {
        const c = structure[k];
        if (c === '(') {
            depths[k] = stack.length;
            stack.push(k);
        } else if (c === ')') {
            if (stack.length > 0) {
                const i = stack.pop()!;
                const depth = depths[i];
                depths[k] = depth;
                pairs.push({ i, j: k, depth });
            }
        }
    }
    return pairs;
}

/**
 * Detect if an object has sequence + structure fields (dot-bracket).
 */
export function isStructureItem(item: Record<string, unknown>): boolean {
    if (typeof item['sequence'] !== 'string' || typeof item['structure'] !== 'string') return false;
    const s = item['structure'] as string;
    if (s.length < 3) return false;
    // Must contain only dot-bracket characters
    for (let i = 0; i < s.length; i++) {
        const c = s[i];
        if (c !== '.' && c !== '(' && c !== ')') return false;
    }
    return true;
}

/**
 * Build an SVG arc diagram for a sequence + dot-bracket structure.
 */
export function buildStructureViewer(sequence: string, structure: string): HTMLElement {
    const wrapper = document.createElement('div');
    wrapper.style.width = '100%';
    wrapper.style.overflow = 'hidden';
    preventDrag(wrapper);

    const n = sequence.length;
    const charW = 9;
    const padLeft = 4;
    const padRight = 4;
    const seqY = 100;
    const svgW = padLeft + n * charW + padRight;
    const svgH = seqY + 18;

    const pairs = parseDotBracket(structure);

    const ns = 'http://www.w3.org/2000/svg';
    const svg = document.createElementNS(ns, 'svg');
    svg.setAttribute('viewBox', `0 0 ${svgW} ${svgH}`);
    svg.style.width = '100%';
    svg.style.maxHeight = '120px';
    svg.style.display = 'block';

    // Arcs
    for (const pair of pairs) {
        const x1 = padLeft + pair.i * charW + charW / 2;
        const x2 = padLeft + pair.j * charW + charW / 2;
        const radius = (x2 - x1) / 2;
        const color = DEPTH_COLORS[pair.depth % DEPTH_COLORS.length];

        const path = document.createElementNS(ns, 'path');
        path.setAttribute('d', `M ${x1} ${seqY} A ${radius} ${radius} 0 0 1 ${x2} ${seqY}`);
        path.setAttribute('fill', 'none');
        path.setAttribute('stroke', color);
        path.setAttribute('stroke-width', '1.2');
        path.setAttribute('opacity', '0.7');
        svg.appendChild(path);
    }

    // Sequence bases
    for (let i = 0; i < n; i++) {
        const base = sequence[i].toUpperCase();
        const x = padLeft + i * charW + charW / 2;
        const color = BASE_COLORS[base] || '#a0a8ad';

        const text = document.createElementNS(ns, 'text');
        text.setAttribute('x', String(x));
        text.setAttribute('y', String(seqY + 12));
        text.setAttribute('text-anchor', 'middle');
        text.setAttribute('font-family', 'monospace');
        text.setAttribute('font-size', '10');
        text.setAttribute('fill', color);
        text.textContent = base;
        svg.appendChild(text);
    }

    // Dot-bracket below sequence
    const dbText = document.createElementNS(ns, 'text');
    dbText.setAttribute('x', String(padLeft));
    dbText.setAttribute('y', String(seqY + 12));
    dbText.setAttribute('font-family', 'monospace');
    dbText.setAttribute('font-size', '9');
    dbText.setAttribute('fill', '#555');
    dbText.setAttribute('opacity', '0');
    svg.appendChild(dbText);

    wrapper.appendChild(svg);
    return wrapper;
}
