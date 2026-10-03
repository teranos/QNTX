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

/** The comet: a head falling toward the earth, its tail fanning back up to the stars it left. */
export function comet(): SVGSVGElement {
    const svg = plate(150, 'comet');
    const hx = 372;
    const hy = 108;
    for (let k = -4; k <= 4; k++) {
        const turn = -0.95 + k * 0.035;
        const long = 240 - Math.abs(k) * 30;
        draw(svg, 'line', {
            class: 'gr-comet-tail',
            x1: hx, y1: hy,
            x2: (hx + Math.cos(turn) * long).toFixed(1), y2: (hy + Math.sin(turn) * long).toFixed(1),
        });
    }
    draw(svg, 'path', { class: 'gr-comet-head', d: burstPath(hx, hy, 16, 9.5, 3.4, 12) });
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
