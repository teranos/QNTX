/**
 * The picture Ground is drawn as: a night over the earth, in cross-section.
 */

// "QNTX IS THE STARS"
// "COMET IS GROUND BINARY BEING BUILT BY QNTX AND LANDING ON EARTH"
// "SKY IS HOW WE TALK TO QNTX AND RECEIVE FROM IT"

// Every plate is drawn 520 wide, the width Ground opens at. Its colours are
// the classes in components.css, so the night and the earth stay tokens.

const SVG = 'http://www.w3.org/2000/svg';
const WIDE = 520;

function plate(height: number, name: string): SVGSVGElement {
    const svg = document.createElementNS(SVG, 'svg');
    svg.setAttribute('class', `gr-plate gr-plate-${name}`);
    svg.setAttribute('viewBox', `0 0 ${WIDE} ${height}`);
    svg.setAttribute('preserveAspectRatio', 'xMidYMin slice');
    svg.setAttribute('aria-hidden', 'true');
    return svg;
}

function draw(into: SVGElement, shape: string, attrs: Record<string, string | number>): void {
    const el = document.createElementNS(SVG, shape);
    for (const [name, value] of Object.entries(attrs)) el.setAttribute(name, String(value));
    into.appendChild(el);
}

// The same seed draws the same sky: a star does not move between two openings.
function seeded(seed: number): () => number {
    let a = seed;
    return () => {
        a = (a + 0x6D2B79F5) | 0;
        let t = Math.imul(a ^ (a >>> 15), 1 | a);
        t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
        return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    };
}

/** A star of rays around a core, as a path: its tips alternate long and short. */
export function burstPath(cx: number, cy: number, long: number, short: number, core: number, rays: number): string {
    const points: string[] = [];
    for (let i = 0; i < rays * 2; i++) {
        const tip = i % 2 === 0;
        const reach = tip ? ((i / 2) % 2 === 0 ? long : short) : core;
        const turn = (i * Math.PI) / rays - Math.PI / 2;
        points.push(`${(cx + Math.cos(turn) * reach).toFixed(2)} ${(cy + Math.sin(turn) * reach).toFixed(2)}`);
    }
    return `M ${points.join(' L ')} Z`;
}

// "Scry should be the strata at the very top the nebula"

/**
 * The nebula as docs/research/probability-nebula.md says it: a cloud of
 * particles about a core, and the path a generation took through it.
 */
export function nebula(): SVGSVGElement {
    const svg = plate(128, 'nebula');
    const next = seeded(23);
    const bell = () => Math.sqrt(-2 * Math.log(1 - next())) * Math.cos(2 * Math.PI * next());
    const cores = [
        { x: 384, y: 60, wide: 62, high: 17, motes: 430 },
        { x: 306, y: 92, wide: 34, high: 9, motes: 150 },
    ];
    for (const core of cores) {
        for (let i = 0; i < core.motes; i++) {
            const across = bell() * core.wide;
            const x = core.x + across;
            const y = core.y + bell() * core.high - across * 0.12;
            const light = `gr-mote gr-mote-${1 + Math.floor(next() * 3)}`;
            draw(svg, 'circle', { class: light, cx: x.toFixed(1), cy: y.toFixed(1), r: (0.35 + next() * 0.85).toFixed(2) });
        }
    }
    for (let ring = 1; ring <= 5; ring++) {
        draw(svg, 'ellipse', { class: 'gr-ring', cx: 384, cy: 60, rx: ring * 24, ry: ring * 8, transform: 'rotate(-7 384 60)' });
    }
    draw(svg, 'polyline', { class: 'gr-trail', points: '300,104 326,88 344,97 362,74 398,80 410,58 384,60' });
    draw(svg, 'path', { class: 'gr-flare', d: burstPath(384, 60, 8, 8, 1.3, 4) });
    return svg;
}

/** The small stars a stratum of the night is strewn with. */
export function starField(height: number, count: number, seed: number): SVGSVGElement {
    const svg = plate(height, 'field');
    const next = seeded(seed);
    for (let i = 0; i < count; i++) {
        const x = next() * WIDE;
        const y = next() * height;
        const size = next();
        const light = `gr-star gr-star-${1 + Math.floor(next() * 3)}`;
        if (size < 0.72) {
            draw(svg, 'circle', { class: light, cx: x.toFixed(1), cy: y.toFixed(1), r: (0.5 + size).toFixed(2) });
        } else {
            const reach = 2.5 + (size - 0.72) * 14;
            draw(svg, 'path', { class: light, d: burstPath(x, y, reach, reach, reach * 0.16, 4) });
        }
    }
    return svg;
}

/** One star hung in the sky: sixteen rays around a core. */
export function starburst(size: number): SVGSVGElement {
    const svg = document.createElementNS(SVG, 'svg');
    svg.setAttribute('class', 'gr-burst');
    svg.setAttribute('viewBox', `0 0 ${size} ${size}`);
    svg.setAttribute('width', String(size));
    svg.setAttribute('height', String(size));
    svg.setAttribute('aria-hidden', 'true');
    const c = size / 2;
    draw(svg, 'path', { class: 'gr-burst-rays', d: burstPath(c, c, c, c * 0.58, c * 0.2, 16) });
    draw(svg, 'circle', { class: 'gr-burst-core', cx: c, cy: c, r: (c * 0.13).toFixed(2) });
    return svg;
}

// "each repository is a comet"
// "more active comets have longer trail"
// "importance is more based on importance in terms of load, like heaviest at top"

/** One repository as a comet: what it is built into, and how main moved. */
export interface Comet {
    repo: string;
    /** Whether a ground has been built from it. */
    built: boolean;
    /** The size of the ground built from it, in MB: how big a head it falls with. */
    size: number;
    /** How often main moved in the last week. */
    moved: number;
    state: 'observed' | 'observing' | 'unobserved';
}

/** The height the comet band is drawn at, and the most comets drawn in it. */
export const COMET_BAND = 300;
export const COMETS_AT_MOST = 10;

// Where each comet falls, the heaviest first and highest: no two on a line,
// and each heading a little off the others, as a shower does.
const COMET_SLOTS = [
    { x: 462, y: 78, turn: 0.02 }, { x: 258, y: 112, turn: -0.03 }, { x: 392, y: 136, turn: 0.04 },
    { x: 118, y: 150, turn: -0.05 }, { x: 322, y: 176, turn: 0.01 }, { x: 470, y: 206, turn: -0.02 },
    { x: 204, y: 212, turn: 0.03 }, { x: 374, y: 240, turn: -0.04 }, { x: 84, y: 244, turn: 0 },
    { x: 276, y: 266, turn: 0.02 },
];

// The tail points up and left, back to the stars the comet left.
const COMET_UP = Math.PI * 1.1;

/** Where a comet's head is on the band, for what is laid over it. */
export interface Head {
    x: number;
    y: number;
    r: number;
}

function dotPath(points: Array<[number, number]>): string {
    return points.map(([x, y]) => `M ${x.toFixed(1)} ${y.toFixed(1)} l 0.01 0`).join(' ');
}

// One comet, engraved: a burst of rays for the head, sized by what it is built
// into, and a tail of stipple dense at the head and thinning to single dots,
// as long as main moved, with a faint fan of hairlines under a heavy one.
function engrave(into: SVGElement, c: Comet, x: number, y: number, turn: number, seed: number, scale: number): Head {
    const next = seeded(seed);
    const tone = `gr-comet-${c.state}`;
    const r = (3 + c.size * 2.6) * scale;
    const long = (26 + c.moved * 2.4) * scale;
    // A light one has no hairlines: its path is drawn with nothing on it.
    const hairs = scale > 1 ? 18 : c.moved >= 40 ? 7 : 0;
    let hair = '';
    for (let k = 0; k < hairs; k++) {
        const t = k / (hairs - 1) - 0.5;
        const a = turn + t * 0.11;
        const reach = long * (1 - Math.abs(t) * 0.8) * (0.7 + next() * 0.3);
        hair += `M ${x} ${y} L ${(x + Math.cos(a) * reach).toFixed(1)} ${(y + Math.sin(a) * reach).toFixed(1)} `;
    }
    draw(into, 'path', { class: `gr-comet-hair ${tone}`, d: hair });
    const grains: Array<Array<[number, number]>> = [[], [], []];
    const motes = Math.round(c.moved * 9 * scale * scale);
    for (let i = 0; i < motes; i++) {
        const far = Math.pow(next(), 1.6);
        const along = r * 0.6 + far * long;
        const spread = (0.7 + along * 0.07) * (0.5 + next() * 0.5);
        const across = (next() - 0.5) * 2 * spread;
        const weight = (1 - far) * next();
        grains[weight < 0.3 ? 0 : weight < 0.65 ? 1 : 2].push([
            x + Math.cos(turn) * along - Math.sin(turn) * across,
            y + Math.sin(turn) * along + Math.cos(turn) * across,
        ]);
    }
    grains.forEach((grain, i) => {
        draw(into, 'path', { class: `gr-comet-grain gr-comet-grain-${i + 1} ${tone}`, d: dotPath(grain) });
    });
    draw(into, 'path', { class: `gr-comet-burst ${tone}`, d: burstPath(x, y, r, r * 0.62, r * 0.26, 16) });
    if (c.size >= 1.5 || scale > 1) {
        let d = '';
        for (let k = 0; k < 4; k++) {
            const a = -Math.PI / 2 + k * Math.PI / 2;
            d += `M ${(x + Math.cos(a) * r).toFixed(1)} ${(y + Math.sin(a) * r).toFixed(1)} L ${(x + Math.cos(a) * r * 1.9).toFixed(1)} ${(y + Math.sin(a) * r * 1.9).toFixed(1)} `;
        }
        draw(into, 'path', { class: `gr-comet-rays ${tone}`, d });
    }
    draw(into, 'path', { class: `gr-comet-core ${tone}`, d: burstPath(x, y, r * 0.3, r * 0.3, r * 0.1, 8) });
    return { x, y, r };
}

/**
 * The comets: one per repository, the heaviest first and highest, each a
 * head falling toward the earth and a tail back up to the stars it left.
 * Returns the band and where each head is, in the order given.
 */
export function comets(list: Comet[]): { svg: SVGSVGElement; heads: Head[] } {
    const svg = plate(COMET_BAND, 'comet');
    const next = seeded(5);
    for (let i = 0; i < 150; i++) {
        const light = next() < 0.85 ? 'gr-star gr-star-1' : 'gr-star gr-star-3';
        draw(svg, 'circle', { class: light, cx: (next() * WIDE).toFixed(1), cy: (next() * COMET_BAND).toFixed(1), r: light.endsWith('1') ? 0.4 : 0.65 });
    }
    const heads: Head[] = [];
    list.slice(0, COMETS_AT_MOST).forEach((c, i) => {
        const slot = COMET_SLOTS[i];
        heads.push(engrave(svg, c, slot.x, slot.y, COMET_UP + slot.turn, 7 + i * 13, 1));
    });
    return { svg, heads };
}

/** One comet alone, large: the Comet element's own picture of a repository. */
export function cometAlone(c: Comet): SVGSVGElement {
    const svg = plate(230, 'comet');
    const next = seeded(9);
    for (let i = 0; i < 90; i++) {
        const light = next() < 0.85 ? 'gr-star gr-star-1' : 'gr-star gr-star-3';
        draw(svg, 'circle', { class: light, cx: (next() * WIDE).toFixed(1), cy: (next() * 230).toFixed(1), r: light.endsWith('1') ? 0.4 : 0.65 });
    }
    engrave(svg, c, 430, 150, Math.PI * 1.13, 21, 2.6);
    return svg;
}

/** The cloud bank the sky begins with: three rows of scallops, each nearer and smaller. */
export function cloudBank(): SVGSVGElement {
    const svg = plate(62, 'clouds');
    const rows = [
        { r: 22, y: 24, depth: 'far' },
        { r: 17, y: 40, depth: 'mid' },
        { r: 13, y: 54, depth: 'near' },
    ];
    rows.forEach((row, i) => {
        const from = -row.r * (1 + (i % 2));
        let d = `M ${from} ${row.y}`;
        for (let x = from; x < WIDE; x += row.r * 2) d += ` a ${row.r} ${row.r} 0 0 1 ${row.r * 2} 0`;
        draw(svg, 'path', { class: `gr-cloud gr-cloud-${row.depth}`, d: `${d} V 80 H ${from} Z` });
    });
    return svg;
}

// "claude and other coding agents are in the sky"

/** One cloud, for one agent in the sky: puffs on a level base, each drawn in rings, the smaller behind the larger. */
export function cloud(wide: number, seed: number): SVGSVGElement {
    const high = Math.round(wide * 0.5);
    const svg = document.createElementNS(SVG, 'svg');
    svg.setAttribute('class', 'gr-cloudlet');
    svg.setAttribute('viewBox', `0 0 ${wide} ${high}`);
    svg.setAttribute('width', String(wide));
    svg.setAttribute('height', String(high));
    svg.setAttribute('aria-hidden', 'true');
    const next = seeded(seed);
    const base = high - 1;
    const puffs: Array<{ cx: number; r: number }> = [];
    for (let i = 0; i < 4; i++) {
        const along = i / 3;
        const middle = 1 - Math.abs(along * 2 - 1);
        puffs.push({ cx: wide * (0.18 + 0.64 * along), r: wide * (0.13 + 0.05 * next()) * (1 + 0.55 * middle) });
    }
    puffs.sort((a, b) => a.r - b.r);
    for (const puff of puffs) {
        for (const ring of [1, 0.72, 0.46, 0.22]) {
            const r = (puff.r * ring).toFixed(1);
            const arc = `M ${(puff.cx - puff.r * ring).toFixed(1)} ${base} A ${r} ${r} 0 0 1 ${(puff.cx + puff.r * ring).toFixed(1)} ${base}`;
            draw(svg, 'path', ring === 1 ? { class: 'gr-puff', d: `${arc} Z` } : { class: 'gr-puff-ring', d: arc });
        }
    }
    return svg;
}

/** The horizon: three hills of contour lines, the far one first so the near ones stand before it. */
export function horizon(): SVGSVGElement {
    const svg = plate(92, 'horizon');
    const hills = [
        { cx: 262, rx: 150, ry: 40 },
        { cx: 66, rx: 176, ry: 76 },
        { cx: 462, rx: 158, ry: 62 },
    ];
    for (const hill of hills) {
        for (let ring = 0; ring < 7; ring++) {
            const k = 1 - ring * 0.135;
            const rx = (hill.rx * k).toFixed(1);
            const ry = (hill.ry * k).toFixed(1);
            draw(svg, 'path', {
                class: ring === 0 ? 'gr-hill' : 'gr-contour',
                d: `M ${(hill.cx - hill.rx * k).toFixed(1)} 92 A ${rx} ${ry} 0 0 1 ${(hill.cx + hill.rx * k).toFixed(1)} 92`,
            });
        }
    }
    return svg;
}

// "AT THE BOTTOM OF THE GROUND ELEMENT IS FIERY RED ERROR RED FATAL RED HIGH ENTROPY MADNESS"
// "BUT ITS SACRED"

/** The core: cracks and sparks with no order to them, and at its heart rings that none of them breaks. */
export function core(): SVGSVGElement {
    const svg = plate(170, 'core');
    const next = seeded(41);
    const heat = () => `gr-ember-${1 + Math.floor(next() * 3)}`;
    for (let crack = 0; crack < 38; crack++) {
        let x = next() * WIDE;
        let y = 170;
        let d = `M ${x.toFixed(1)} ${y}`;
        const turns = 4 + Math.floor(next() * 9);
        for (let turn = 0; turn < turns; turn++) {
            x += (next() - 0.5) * 48;
            y -= 5 + next() * 21;
            d += ` L ${x.toFixed(1)} ${y.toFixed(1)}`;
        }
        draw(svg, 'path', { class: `gr-crack ${heat()}`, d });
    }
    for (let spark = 0; spark < 96; spark++) {
        const size = (1 + next() * 2.6).toFixed(1);
        draw(svg, 'rect', { class: `gr-spark ${heat()}`, x: (next() * WIDE).toFixed(1), y: (next() * 170).toFixed(1), width: size, height: size });
    }
    for (let ring = 0; ring < 7; ring++) {
        const r = 86 - ring * 11;
        const arc = `M ${260 - r} 170 A ${r} ${r} 0 0 1 ${260 + r} 170`;
        draw(svg, 'path', ring === 0 ? { class: 'gr-heart', d: `${arc} Z` } : { class: 'gr-heart-ring', d: arc });
    }
    return svg;
}

/** A seam between two layers of earth: three contour lines, no two seams alike. */
export function seam(seed: number): SVGSVGElement {
    const svg = plate(22, 'seam');
    const next = seeded(seed);
    for (let line = 0; line < 3; line++) {
        let d = `M 0 ${5 + line * 6}`;
        for (let x = 0; x < WIDE; x += 65) d += ` q 32.5 ${((next() - 0.5) * 8).toFixed(1)} 65 0`;
        draw(svg, 'path', { class: 'gr-contour', d });
    }
    return svg;
}
