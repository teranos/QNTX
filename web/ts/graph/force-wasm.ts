// WASM Force Simulation Bridge
// Provides D3-compatible interface backed by Zig WASM module
//
// Usage:
//   const sim = await ForceSimulation.create(nodes, links);
//   sim.on('tick', () => updatePositions(sim.nodes()));
//   sim.alpha(0.3).restart();

import type { D3Node, D3Link } from '../../types/d3-graph';

// WASM memory layout matches force.zig structs
const NODE_SIZE = 28; // 7 x f32: x, y, vx, vy, fx, fy, radius
const LINK_SIZE = 16; // 2 x u32 + 2 x f32: source, target, distance, strength

interface WasmExports {
    memory: WebAssembly.Memory;
    init(nodeCount: number, linkCount: number): number;
    getLinksPtr(): number;
    setCenter(x: number, y: number): void;
    setChargeStrength(strength: number): void;
    setCollisionRadius(nodeIndex: number, radius: number): void;
    fixNode(nodeIndex: number, x: number, y: number): void;
    unfixNode(nodeIndex: number): void;
    tick(alpha: number): void;
    getNodeCount(): number;
    getLinkCount(): number;
}

type TickCallback = () => void;

export class ForceSimulation {
    private wasm: WasmExports;
    private nodesPtr: number;
    private linksPtr: number;
    private nodeMap: Map<string, number>; // id -> index
    private nodeList: D3Node[];
    private linkList: D3Link[];
    private _alpha: number = 1;
    private _alphaTarget: number = 0;
    private _alphaDecay: number = 0.0228; // ~300 ticks to cool
    private _alphaMin: number = 0.001;
    private tickCallbacks: TickCallback[] = [];
    private running: boolean = false;
    private animationFrame: number | null = null;

    private constructor(wasm: WasmExports) {
        this.wasm = wasm;
        this.nodesPtr = 0;
        this.linksPtr = 0;
        this.nodeMap = new Map();
        this.nodeList = [];
        this.linkList = [];
    }

    /**
     * Create a new force simulation with the given nodes and links
     */
    static async create(nodes: D3Node[], links: D3Link[]): Promise<ForceSimulation> {
        const wasmPath = '/wasm/dist/force.wasm';

        const response = await fetch(wasmPath);
        if (!response.ok) {
            throw new Error(`Failed to load WASM: ${response.status} ${response.statusText}`);
        }

        const wasmBytes = await response.arrayBuffer();
        const wasmModule = await WebAssembly.instantiate(wasmBytes, {
            env: {
                // No imports needed for this simple module
            }
        });

        const wasm = wasmModule.instance.exports as unknown as WasmExports;
        const sim = new ForceSimulation(wasm);
        sim.setData(nodes, links);
        return sim;
    }

    /**
     * Set simulation data (nodes and links)
     */
    setData(nodes: D3Node[], links: D3Link[]): this {
        this.nodeList = nodes;
        this.linkList = links;

        // Build node index map
        this.nodeMap.clear();
        nodes.forEach((node, i) => {
            this.nodeMap.set(node.id, i);
        });

        // Initialize WASM memory
        this.nodesPtr = this.wasm.init(nodes.length, links.length);
        this.linksPtr = this.wasm.getLinksPtr();

        // Copy node data to WASM
        const mem = new DataView(this.wasm.memory.buffer);
        nodes.forEach((node, i) => {
            const offset = this.nodesPtr + i * NODE_SIZE;
            mem.setFloat32(offset + 0, node.x ?? Math.random() * 100, true);
            mem.setFloat32(offset + 4, node.y ?? Math.random() * 100, true);
            mem.setFloat32(offset + 8, node.vx ?? 0, true);
            mem.setFloat32(offset + 12, node.vy ?? 0, true);
            mem.setFloat32(offset + 16, NaN, true); // fx
            mem.setFloat32(offset + 20, NaN, true); // fy
            mem.setFloat32(offset + 24, 60, true);  // radius
        });

        // Copy link data to WASM
        links.forEach((link, i) => {
            const sourceId = typeof link.source === 'string' ? link.source : link.source.id;
            const targetId = typeof link.target === 'string' ? link.target : link.target.id;
            const sourceIdx = this.nodeMap.get(sourceId) ?? 0;
            const targetIdx = this.nodeMap.get(targetId) ?? 0;

            const offset = this.linksPtr + i * LINK_SIZE;
            mem.setUint32(offset + 0, sourceIdx, true);
            mem.setUint32(offset + 4, targetIdx, true);
            mem.setFloat32(offset + 8, (link as any).distance ?? 100, true);
            mem.setFloat32(offset + 12, (link as any).strength ?? 0.1, true);
        });

        return this;
    }

    /**
     * Register tick callback
     */
    on(event: 'tick' | 'end', callback: TickCallback): this {
        if (event === 'tick') {
            this.tickCallbacks.push(callback);
        }
        return this;
    }

    /**
     * Set simulation alpha (temperature)
     */
    alpha(value?: number): number | this {
        if (value === undefined) return this._alpha;
        this._alpha = value;
        return this;
    }

    /**
     * Set alpha target (what alpha decays toward)
     */
    alphaTarget(value?: number): number | this {
        if (value === undefined) return this._alphaTarget;
        this._alphaTarget = value;
        return this;
    }

    /**
     * Set center force position
     */
    center(x: number, y: number): this {
        this.wasm.setCenter(x, y);
        return this;
    }

    /**
     * Configure charge force
     */
    charge(strength: number): this {
        this.wasm.setChargeStrength(strength);
        return this;
    }

    /**
     * Fix a node's position (for dragging)
     */
    fix(nodeId: string, x: number, y: number): this {
        const idx = this.nodeMap.get(nodeId);
        if (idx !== undefined) {
            this.wasm.fixNode(idx, x, y);
        }
        return this;
    }

    /**
     * Unfix a node's position
     */
    unfix(nodeId: string): this {
        const idx = this.nodeMap.get(nodeId);
        if (idx !== undefined) {
            this.wasm.unfixNode(idx);
        }
        return this;
    }

    /**
     * Start/restart simulation
     */
    restart(): this {
        if (this.running) return this;
        this.running = true;
        this.tick_loop();
        return this;
    }

    /**
     * Stop simulation
     */
    stop(): this {
        this.running = false;
        if (this.animationFrame !== null) {
            cancelAnimationFrame(this.animationFrame);
            this.animationFrame = null;
        }
        return this;
    }

    /**
     * Get current node positions (reads from WASM memory)
     */
    nodes(): D3Node[] {
        const mem = new DataView(this.wasm.memory.buffer);

        this.nodeList.forEach((node, i) => {
            const offset = this.nodesPtr + i * NODE_SIZE;
            node.x = mem.getFloat32(offset + 0, true);
            node.y = mem.getFloat32(offset + 4, true);
            node.vx = mem.getFloat32(offset + 8, true);
            node.vy = mem.getFloat32(offset + 12, true);
        });

        return this.nodeList;
    }

    /**
     * Main tick loop
     */
    private tick_loop(): void {
        if (!this.running) return;

        // Decay alpha toward target
        this._alpha += (this._alphaTarget - this._alpha) * this._alphaDecay;

        // Run physics tick in WASM
        this.wasm.tick(this._alpha);

        // Update JS node positions from WASM memory
        this.nodes();

        // Notify callbacks
        for (const cb of this.tickCallbacks) {
            cb();
        }

        // Stop if cooled
        if (this._alpha < this._alphaMin) {
            this.running = false;
            return;
        }

        // Continue
        this.animationFrame = requestAnimationFrame(() => this.tick_loop());
    }
}

/**
 * Check if WASM force simulation is available
 */
export async function isWasmForceAvailable(): Promise<boolean> {
    try {
        const response = await fetch('/wasm/dist/force.wasm', { method: 'HEAD' });
        return response.ok;
    } catch {
        return false;
    }
}
