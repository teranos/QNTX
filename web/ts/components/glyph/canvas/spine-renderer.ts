/**
 * Spine Renderer — persistent SVG thread lines on the canvas.
 *
 * Draws flowing Bezier curves between connected glyph symbols.
 * Always visible at ~12% opacity (whisper-quiet). Hover brightens.
 * Updates positions when glyphs move via MutationObserver on style changes.
 */

import { log, SEG } from '../../../logger';

/** Whisper-quiet opacity for resting threads (TODO: lower to 0.12 for production) */
const REST_OPACITY = 0.5;

/** Brightened opacity on hover */
const HOVER_OPACITY = 0.5;

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
    path: SVGPathElement;
}

/** Per-canvas spine renderer state */
const renderers = new Map<string, {
    svg: SVGSVGElement;
    spines: RenderedSpine[];
    animId: number;
}>();

/**
 * Build a cubic Bezier `d` through multiple points.
 * Curves bow outward perpendicular to the line between points,
 * alternating sides so consecutive segments don't overlap.
 */
function buildSpinePath(points: { x: number; y: number }[]): string {
    if (points.length < 2) return '';

    const segments: string[] = [];
    for (let i = 0; i < points.length - 1; i++) {
        const a = points[i];
        const b = points[i + 1];
        const dx = b.x - a.x;
        const dy = b.y - a.y;
        const dist = Math.sqrt(dx * dx + dy * dy);

        // Perpendicular direction (normalized), alternating side
        const side = i % 2 === 0 ? 1 : -1;
        const bow = Math.min(dist * 0.25, 60) * side;
        const nx = -dy / (dist || 1);
        const ny = dx / (dist || 1);

        // Control points offset perpendicular to the line
        const mx = (a.x + b.x) / 2 + nx * bow;
        const my = (a.y + b.y) / 2 + ny * bow;

        const cx1 = a.x + (mx - a.x) * 0.6;
        const cy1 = a.y + (my - a.y) * 0.6;
        const cx2 = b.x + (mx - b.x) * 0.6;
        const cy2 = b.y + (my - b.y) * 0.6;

        segments.push(`M ${a.x} ${a.y} C ${cx1} ${cy1}, ${cx2} ${cy2}, ${b.x} ${b.y}`);
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

/** Ensure SVG overlay exists for this canvas */
function ensureRenderer(canvasId: string, canvas: HTMLElement): { svg: SVGSVGElement; spines: RenderedSpine[]; animId: number } {
    let renderer = renderers.get(canvasId);
    if (renderer) return renderer;

    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
    svg.style.position = 'absolute';
    svg.style.inset = '0';
    svg.style.width = '100%';
    svg.style.height = '100%';
    svg.style.pointerEvents = 'none';
    svg.style.overflow = 'visible';
    // Append to canvas with high z-index — renders above glyphs
    svg.style.zIndex = '9999';
    canvas.appendChild(svg);

    renderer = { svg, spines: [], animId: 0 };
    renderers.set(canvasId, renderer);

    // Periodic position update (handles drag, pan, zoom)
    const update = () => {
        for (const rs of renderer!.spines) {
            const points: { x: number; y: number }[] = [];
            for (const nodeId of rs.spine.nodes) {
                const p = getSymbolCenter(canvas, nodeId);
                if (p) points.push(p);
            }
            rs.path.setAttribute('d', buildSpinePath(points));
        }
        renderer!.animId = requestAnimationFrame(update);
    };
    renderer.animId = requestAnimationFrame(update);

    return renderer;
}

/** Add a spine to the canvas renderer */
export function addSpine(canvasId: string, canvas: HTMLElement, spine: Spine): void {
    const renderer = ensureRenderer(canvasId, canvas);

    const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    path.setAttribute('fill', 'none');
    path.setAttribute('stroke', spine.color);
    path.setAttribute('stroke-width', String(LINE_WIDTH));
    path.setAttribute('stroke-opacity', String(REST_OPACITY));
    path.setAttribute('stroke-linecap', 'round');
    path.style.transition = 'stroke-opacity 150ms ease';
    path.style.pointerEvents = 'stroke';

    // Hover brightens
    path.addEventListener('mouseenter', () => {
        path.setAttribute('stroke-opacity', String(HOVER_OPACITY));
    });
    path.addEventListener('mouseleave', () => {
        path.setAttribute('stroke-opacity', String(REST_OPACITY));
    });

    renderer.svg.appendChild(path);
    renderer.spines.push({ spine, path });

    log.debug(SEG.GLYPH, `[Spine] Added spine ${spine.id} with ${spine.nodes.length} nodes, color ${spine.color}`);
}

/** Remove a spine from the canvas renderer */
export function removeSpine(canvasId: string, spineId: string): void {
    const renderer = renderers.get(canvasId);
    if (!renderer) return;

    const idx = renderer.spines.findIndex(rs => rs.spine.id === spineId);
    if (idx < 0) return;

    renderer.spines[idx].path.remove();
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
