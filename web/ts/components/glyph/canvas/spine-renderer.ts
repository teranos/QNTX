/**
 * Spine Renderer — persistent SVG thread lines on the canvas.
 *
 * Draws flowing Bezier curves between connected glyph symbols.
 * Always visible at ~12% opacity (whisper-quiet). Hover brightens.
 * Updates positions when glyphs move via MutationObserver on style changes.
 */

import { log, SEG } from '../../../logger';

/** Bright opacity for the under-glyph layer (visible over canvas, occluded by glyphs) */
const UNDER_OPACITY = 0.55;

/** Whisper-quiet opacity for the over-glyph layer (subtle hint where line crosses a glyph) */
const OVER_OPACITY = 0.12;

/** Stroke width */
const LINE_WIDTH = 2.5;

export interface Spine {
    id: string;
    color: string;
    /** Glyph IDs in thread order */
    nodes: string[];
}

interface RenderedSpine {
    spine: Spine;
    /** Bright path drawn under the glyphs — visible over canvas, occluded by glyph wrappers */
    underPath: SVGPathElement;
    /** Faint baseline drawn above everything — subtle hint where the thread crosses a glyph */
    overPath: SVGPathElement;
}

/** Per-canvas spine renderer state */
const renderers = new Map<string, {
    /** SVG behind the glyphs — glyph wrappers occlude this layer */
    svgUnder: SVGSVGElement;
    /** SVG above the glyphs — drawn on top of everything */
    svgOver: SVGSVGElement;
    spines: RenderedSpine[];
    animId: number;
}>();

/**
 * Build a smooth cubic Bezier `d` through all points via a Catmull-Rom
 * spline (tangents derived from neighbouring points). One continuous
 * curve — no per-segment bowing, no alternating sides.
 */
function buildSpinePath(points: { x: number; y: number }[]): string {
    if (points.length < 2) return '';
    if (points.length === 2) {
        return `M ${points[0].x} ${points[0].y} L ${points[1].x} ${points[1].y}`;
    }

    const segments: string[] = [`M ${points[0].x} ${points[0].y}`];
    for (let i = 0; i < points.length - 1; i++) {
        const p0 = points[i === 0 ? 0 : i - 1];
        const p1 = points[i];
        const p2 = points[i + 1];
        const p3 = points[i + 2 < points.length ? i + 2 : i + 1];

        // Catmull-Rom to Bezier control points (uniform parameterisation, t=0.5 tension)
        const cp1x = p1.x + (p2.x - p0.x) / 6;
        const cp1y = p1.y + (p2.y - p0.y) / 6;
        const cp2x = p2.x - (p3.x - p1.x) / 6;
        const cp2y = p2.y - (p3.y - p1.y) / 6;

        segments.push(`C ${cp1x} ${cp1y}, ${cp2x} ${cp2y}, ${p2.x} ${p2.y}`);
    }
    return segments.join(' ');
}

/** Find the .glyph-symbol center within a canvas glyph, in content-layer coordinates */
function getSymbolCenter(canvas: HTMLElement, glyphId: string): { x: number; y: number } | null {
    const glyphEl = canvas.querySelector(`[data-glyph-id="${glyphId}"]`) as HTMLElement | null;
    if (!glyphEl) return null;

    const symbolEl = glyphEl.querySelector('.glyph-symbol') as HTMLElement | null;
    const target = symbolEl ?? glyphEl;

    // Use offsetLeft/offsetTop to get content-layer coordinates (pre-transform)
    // Walk up from target to canvas to accumulate offsets
    let x = target.offsetWidth / 2;
    let y = target.offsetHeight / 2;
    let el: HTMLElement | null = target;
    while (el && el !== canvas) {
        x += el.offsetLeft;
        y += el.offsetTop;
        el = el.offsetParent as HTMLElement | null;
    }

    return { x, y };
}

function makeSpineSvg(): SVGSVGElement {
    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.style.position = 'absolute';
    svg.style.inset = '0';
    svg.style.width = '100%';
    svg.style.height = '100%';
    svg.style.pointerEvents = 'none';
    svg.style.overflow = 'visible';
    return svg;
}

/** Ensure both spine SVG layers exist for this canvas */
function ensureRenderer(canvasId: string, canvas: HTMLElement): { svgUnder: SVGSVGElement; svgOver: SVGSVGElement; spines: RenderedSpine[]; animId: number } {
    let renderer = renderers.get(canvasId);
    if (renderer) return renderer;

    // Under-layer: behind glyphs. Insert as first child so glyph wrappers stack above it.
    const svgUnder = makeSpineSvg();
    canvas.insertBefore(svgUnder, canvas.firstChild);

    // Over-layer: above everything. Append at end with explicit high z-index.
    const svgOver = makeSpineSvg();
    svgOver.style.zIndex = '9999';
    canvas.appendChild(svgOver);

    renderer = { svgUnder, svgOver, spines: [], animId: 0 };
    renderers.set(canvasId, renderer);

    // Periodic position update (handles drag, pan, zoom)
    const update = () => {
        for (const rs of renderer!.spines) {
            const points: { x: number; y: number }[] = [];
            for (const nodeId of rs.spine.nodes) {
                const p = getSymbolCenter(canvas, nodeId);
                if (p) points.push(p);
            }
            const d = buildSpinePath(points);
            rs.underPath.setAttribute('d', d);
            rs.overPath.setAttribute('d', d);
        }
        renderer!.animId = requestAnimationFrame(update);
    };
    renderer.animId = requestAnimationFrame(update);

    return renderer;
}

function makeSpinePath(color: string, opacity: number): SVGPathElement {
    const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    path.setAttribute('fill', 'none');
    path.setAttribute('stroke', color);
    path.setAttribute('stroke-width', String(LINE_WIDTH));
    path.setAttribute('stroke-opacity', String(opacity));
    path.setAttribute('stroke-linecap', 'round');
    return path;
}

/** Add a spine to the canvas renderer */
export function addSpine(canvasId: string, canvas: HTMLElement, spine: Spine): void {
    const renderer = ensureRenderer(canvasId, canvas);

    const underPath = makeSpinePath(spine.color, UNDER_OPACITY);
    const overPath = makeSpinePath(spine.color, OVER_OPACITY);
    renderer.svgUnder.appendChild(underPath);
    renderer.svgOver.appendChild(overPath);
    renderer.spines.push({ spine, underPath, overPath });

    log.debug(SEG.GLYPH, `[Spine] Added spine ${spine.id} with ${spine.nodes.length} nodes, color ${spine.color}`);
}

/** Remove a spine from the canvas renderer */
export function removeSpine(canvasId: string, spineId: string): void {
    const renderer = renderers.get(canvasId);
    if (!renderer) return;

    const idx = renderer.spines.findIndex(rs => rs.spine.id === spineId);
    if (idx < 0) return;

    renderer.spines[idx].underPath.remove();
    renderer.spines[idx].overPath.remove();
    renderer.spines.splice(idx, 1);
    log.debug(SEG.GLYPH, `[Spine] Removed spine ${spineId}`);
}

/** Find a spine that contains a given glyph ID */
export function getSpineByNode(canvasId: string, glyphId: string): Spine | null {
    const renderer = renderers.get(canvasId);
    if (!renderer) return null;

    const rs = renderer.spines.find(rs => rs.spine.nodes.includes(glyphId));
    return rs?.spine ?? null;
}

/** All spines that contain a given glyph ID, in renderer (≈ creation) order */
export function getSpinesByNode(canvasId: string, glyphId: string): Spine[] {
    const renderer = renderers.get(canvasId);
    if (!renderer) return [];
    return renderer.spines.filter(rs => rs.spine.nodes.includes(glyphId)).map(rs => rs.spine);
}
