/**
 * Graph View Module — WebGL graph visualization via regl.
 *
 * Fetches graph data from GET /api/graph, runs a force-directed layout,
 * renders nodes as circles and edges as lines. Pan/zoom via mouse.
 */

import createREGL from 'regl';

// Node type color palette — deterministic mapping from type string to color
const PALETTE = [
    [0.2, 1.0, 0.4],   // green
    [0.4, 0.6, 1.0],   // blue
    [1.0, 0.4, 0.4],   // red
    [1.0, 0.8, 0.2],   // yellow
    [0.8, 0.4, 1.0],   // purple
    [0.2, 0.9, 0.9],   // cyan
    [1.0, 0.6, 0.2],   // orange
    [0.6, 1.0, 0.6],   // light green
];

interface GraphNode {
    id: string;
    type: string;
    label: string;
    group?: number;
    // Layout state
    x: number;
    y: number;
    vx: number;
    vy: number;
}

interface GraphLink {
    source: string;
    target: string;
    type: string;
    value: number;
}

interface GraphData {
    nodes: GraphNode[];
    links: GraphLink[];
    meta: any;
}

function hashType(type: string): number {
    let h = 0;
    for (let i = 0; i < type.length; i++) {
        h = ((h << 5) - h + type.charCodeAt(i)) | 0;
    }
    return Math.abs(h);
}

function colorForType(type: string): number[] {
    return PALETTE[hashType(type) % PALETTE.length];
}

// ── Force layout ────────────────────────────────────────────────────

function initPositions(nodes: GraphNode[]) {
    for (const n of nodes) {
        n.x = (Math.random() - 0.5) * 2;
        n.y = (Math.random() - 0.5) * 2;
        n.vx = 0;
        n.vy = 0;
    }
}

// Normalize positions to fill [-0.8, 0.8] clip space
function normalizePositions(nodes: GraphNode[]) {
    if (nodes.length === 0) return;
    let minX = Infinity, maxX = -Infinity, minY = Infinity, maxY = -Infinity;
    for (const n of nodes) {
        if (n.x < minX) minX = n.x;
        if (n.x > maxX) maxX = n.x;
        if (n.y < minY) minY = n.y;
        if (n.y > maxY) maxY = n.y;
    }
    const rangeX = maxX - minX || 1;
    const rangeY = maxY - minY || 1;
    const scale = 1.6 / Math.max(rangeX, rangeY);
    const cx = (minX + maxX) / 2;
    const cy = (minY + maxY) / 2;
    for (const n of nodes) {
        n.x = (n.x - cx) * scale;
        n.y = (n.y - cy) * scale;
    }
}

function runForceLayout(nodes: GraphNode[], links: GraphLink[], ticks: number) {
    const nodeMap = new Map<string, GraphNode>();
    for (const n of nodes) nodeMap.set(n.id, n);

    // Scale repulsion down for large graphs to prevent blowout
    const repulsion = Math.min(0.8, 20 / nodes.length);
    const attraction = 0.15;
    const gravity = 0.08;
    const damping = 0.88;

    for (let t = 0; t < ticks; t++) {
        // Node-node repulsion (coulomb)
        for (let i = 0; i < nodes.length; i++) {
            for (let j = i + 1; j < nodes.length; j++) {
                const a = nodes[i];
                const b = nodes[j];
                let dx = a.x - b.x;
                let dy = a.y - b.y;
                let dist = Math.sqrt(dx * dx + dy * dy);
                if (dist < 0.01) dist = 0.01;
                const force = repulsion / (dist * dist);
                const fx = (dx / dist) * force;
                const fy = (dy / dist) * force;
                a.vx += fx;
                a.vy += fy;
                b.vx -= fx;
                b.vy -= fy;
            }
        }

        // Edge attraction (spring)
        for (const link of links) {
            const a = nodeMap.get(link.source);
            const b = nodeMap.get(link.target);
            if (!a || !b) continue;
            const dx = b.x - a.x;
            const dy = b.y - a.y;
            const dist = Math.sqrt(dx * dx + dy * dy);
            if (dist < 0.001) continue;
            const force = dist * attraction;
            const fx = (dx / dist) * force;
            const fy = (dy / dist) * force;
            a.vx += fx;
            a.vy += fy;
            b.vx -= fx;
            b.vy -= fy;
        }

        // Center gravity
        for (const n of nodes) {
            n.vx -= n.x * gravity;
            n.vy -= n.y * gravity;
        }

        // Apply velocity with damping
        for (const n of nodes) {
            n.vx *= damping;
            n.vy *= damping;
            n.x += n.vx;
            n.y += n.vy;
        }
    }
}

// ── Render ───────────────────────────────────────────────────────────

export function render(glyph: any, ui: any): HTMLElement {
    const container = document.createElement('div');
    container.style.cssText = 'width: 100%; height: 100%; overflow: hidden; background: #0a0a0f; position: relative;';

    const canvas = document.createElement('canvas');
    container.appendChild(canvas);

    // Status overlay for empty state
    const status = document.createElement('div');
    status.style.cssText = 'position: absolute; top: 50%; left: 50%; transform: translate(-50%, -50%); color: #555; font-family: monospace; font-size: 14px; pointer-events: none;';
    status.textContent = 'Fetching graph...';
    container.appendChild(status);

    let pan = [0, 0];
    let zoom = 1.0;
    let regl: any = null;
    let drawNodes: any = null;
    let drawEdges: any = null;
    let currentNodes: GraphNode[] = [];
    let currentLinks: GraphLink[] = [];
    let nodePositions: number[] = [];
    let nodeColors: number[] = [];
    let edgePositions: number[] = [];
    let edgeColors: number[] = [];
    let pollTimer: ReturnType<typeof setInterval> | null = null;
    let destroyed = false;
    let dragging = false;
    let lastMouse = [0, 0];
    let frameHandle: any = null;

    function buildBuffers() {
        // Node positions and colors
        nodePositions = [];
        nodeColors = [];
        for (const n of currentNodes) {
            nodePositions.push(n.x, n.y);
            const c = colorForType(n.type);
            nodeColors.push(c[0], c[1], c[2]);
        }

        // Edge positions and colors (pairs of vertices, each gets same color)
        edgePositions = [];
        edgeColors = [];
        const nodeMap = new Map<string, GraphNode>();
        for (const n of currentNodes) nodeMap.set(n.id, n);
        for (const link of currentLinks) {
            const a = nodeMap.get(link.source);
            const b = nodeMap.get(link.target);
            if (!a || !b) continue;
            edgePositions.push(a.x, a.y, b.x, b.y);
            const c = colorForType(link.type || 'default');
            // Both vertices of the line segment get the same color
            edgeColors.push(c[0], c[1], c[2], c[0], c[1], c[2]);
        }
    }

    function initRegl() {
        const rect = container.getBoundingClientRect();
        if (rect.width === 0 || rect.height === 0) return;

        canvas.width = rect.width * devicePixelRatio;
        canvas.height = rect.height * devicePixelRatio;
        canvas.style.cssText = 'width: 100%; height: 100%; display: block;';

        regl = createREGL({ canvas });

        drawNodes = regl({
            vert: `
                precision mediump float;
                attribute vec2 position;
                attribute vec3 color;
                varying vec3 vColor;
                uniform vec2 pan;
                uniform float zoom;
                uniform float pointSize;
                void main() {
                    vColor = color;
                    vec2 p = (position + pan) * zoom;
                    gl_PointSize = pointSize * zoom;
                    gl_Position = vec4(p, 0.0, 1.0);
                }
            `,
            frag: `
                precision mediump float;
                varying vec3 vColor;
                void main() {
                    vec2 coord = gl_PointCoord - vec2(0.5);
                    if (dot(coord, coord) > 0.25) discard;
                    gl_FragColor = vec4(vColor, 1.0);
                }
            `,
            attributes: {
                position: regl.prop('positions'),
                color: regl.prop('colors'),
            },
            uniforms: {
                pan: regl.prop('pan'),
                zoom: regl.prop('zoom'),
                pointSize: regl.prop('pointSize'),
            },
            count: regl.prop('count'),
            primitive: 'points',
        });

        drawEdges = regl({
            vert: `
                precision mediump float;
                attribute vec2 position;
                attribute vec3 color;
                varying vec3 vColor;
                uniform vec2 pan;
                uniform float zoom;
                void main() {
                    vColor = color;
                    vec2 p = (position + pan) * zoom;
                    gl_Position = vec4(p, 0.0, 1.0);
                }
            `,
            frag: `
                precision mediump float;
                varying vec3 vColor;
                void main() {
                    gl_FragColor = vec4(vColor * 0.6, 0.5);
                }
            `,
            attributes: {
                position: regl.prop('positions'),
                color: regl.prop('colors'),
            },
            uniforms: {
                pan: regl.prop('pan'),
                zoom: regl.prop('zoom'),
            },
            count: regl.prop('count'),
            primitive: 'lines',
            blend: {
                enable: true,
                func: { srcRGB: 'src alpha', dstRGB: 'one minus src alpha', srcAlpha: 'one', dstAlpha: 'one minus src alpha' },
            },
        });

        renderFrame();
    }

    function renderFrame() {
        if (destroyed || !regl) return;

        regl.clear({ color: [0.04, 0.04, 0.06, 1.0], depth: 1 });

        // Draw edges first (behind nodes)
        if (edgePositions.length > 0) {
            drawEdges({
                positions: edgePositions,
                colors: edgeColors,
                pan: pan,
                zoom: zoom,
                count: edgePositions.length / 2,
            });
        }

        // Draw nodes
        if (nodePositions.length > 0) {
            drawNodes({
                positions: nodePositions,
                colors: nodeColors,
                pan: pan,
                zoom: zoom,
                pointSize: 16.0,
                count: nodePositions.length / 2,
            });
        }

        frameHandle = requestAnimationFrame(renderFrame);
    }

    function processGraph(data: any) {
        if (!data || !data.nodes || data.nodes.length === 0) {
            status.textContent = 'No graph data — run an Ax query';
            status.style.display = '';
            currentNodes = [];
            currentLinks = [];
            nodePositions = [];
            nodeColors = [];
            edgePositions = [];
            edgeColors = [];
            return;
        }

        status.style.display = 'none';

        // Map raw nodes to layout nodes, preserving positions if IDs match
        const oldMap = new Map<string, GraphNode>();
        for (const n of currentNodes) oldMap.set(n.id, n);

        const nodes: GraphNode[] = data.nodes
            .filter((n: any) => n.visible !== false)
            .map((n: any) => {
                const old = oldMap.get(n.id);
                return {
                    id: n.id,
                    type: n.type || 'untyped',
                    label: n.label || n.id,
                    group: n.group,
                    x: old ? old.x : 0,
                    y: old ? old.y : 0,
                    vx: 0,
                    vy: 0,
                };
            });

        const links: GraphLink[] = (data.links || [])
            .filter((l: any) => !l.hidden)
            .map((l: any) => ({
                source: l.source,
                target: l.target,
                type: l.type || '',
                value: l.value || 1,
            }));

        // Only re-run layout if node set changed
        const oldIds = new Set(currentNodes.map(n => n.id));
        const newIds = new Set(nodes.map(n => n.id));
        const changed = oldIds.size !== newIds.size || [...newIds].some(id => !oldIds.has(id));

        currentNodes = nodes;
        currentLinks = links;

        if (changed) {
            initPositions(currentNodes);
            runForceLayout(currentNodes, currentLinks, 300);
            normalizePositions(currentNodes);
        }

        buildBuffers();
    }

    async function fetchGraph() {
        if (destroyed) return;
        try {
            const resp = await fetch('/api/graph');
            if (!resp.ok) {
                status.textContent = 'Failed to fetch graph: ' + resp.status;
                status.style.display = '';
                return;
            }
            const data = await resp.json();
            processGraph(data);
        } catch (err) {
            if (!destroyed) {
                status.textContent = 'Network error fetching graph';
                status.style.display = '';
            }
        }
    }

    // Mouse interaction: pan and zoom
    canvas.addEventListener('wheel', (e) => {
        e.preventDefault();
        const factor = e.deltaY > 0 ? 0.9 : 1.1;
        zoom *= factor;
        zoom = Math.max(0.1, Math.min(zoom, 20));
    }, { passive: false });

    canvas.addEventListener('mousedown', (e) => {
        if (e.button === 0) {
            dragging = true;
            lastMouse = [e.clientX, e.clientY];
            e.stopPropagation();
        }
    });

    canvas.addEventListener('mousemove', (e) => {
        if (!dragging) return;
        const rect = canvas.getBoundingClientRect();
        // Convert pixel delta to clip-space delta
        const dx = (e.clientX - lastMouse[0]) / rect.width * 2;
        const dy = -(e.clientY - lastMouse[1]) / rect.height * 2;
        pan[0] += dx / zoom;
        pan[1] += dy / zoom;
        lastMouse = [e.clientX, e.clientY];
    });

    canvas.addEventListener('mouseup', () => { dragging = false; });
    canvas.addEventListener('mouseleave', () => { dragging = false; });

    // Initialize once mounted
    requestAnimationFrame(() => {
        if (destroyed) return;
        initRegl();
        fetchGraph();
        pollTimer = setInterval(fetchGraph, 2000);
    });

    // Resize handling
    const observer = new ResizeObserver(() => {
        if (destroyed || !regl) return;
        const r = container.getBoundingClientRect();
        if (r.width === 0 || r.height === 0) return;
        canvas.width = r.width * devicePixelRatio;
        canvas.height = r.height * devicePixelRatio;
        regl.poll();
    });
    observer.observe(container);

    // Cleanup
    ui.onCleanup(() => {
        destroyed = true;
        if (pollTimer) clearInterval(pollTimer);
        if (frameHandle) cancelAnimationFrame(frameHandle);
        observer.disconnect();
        if (regl) regl.destroy();
    });

    return container;
}
