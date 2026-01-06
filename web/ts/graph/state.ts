// Graph module state management
// Centralized state for D3.js graph visualization

import type { GraphData, Transform } from '../../types/core';
import type {
    ForceSimulation,
    SVGSelection,
    GroupSelection,
    ZoomBehavior
} from '../../types/d3-graph';

// D3 instance references
let simulation: ForceSimulation | null = null;
let svg: SVGSelection | null = null;
let g: GroupSelection | null = null;
let zoom: ZoomBehavior | null = null;

// Individual node visibility state (by node ID)
const hiddenNodes = new Set<string>();

// DOM cache interface for performance optimization
interface DOMCache {
    graphContainer: HTMLElement | null;
    isolatedToggle: HTMLElement | null;
    legenda: HTMLElement | null;
    get(key: keyof DOMCache, selector: string): HTMLElement | null;
    clear(): void;
}

const domCache: DOMCache = {
    graphContainer: null,
    isolatedToggle: null,
    legenda: null,
    get: function(key: keyof DOMCache, selector: string): HTMLElement | null {
        if (!this[key]) {
            const element = document.getElementById(selector) || document.querySelector(selector) as HTMLElement | null;
            (this as any)[key] = element;
        }
        return this[key] as HTMLElement | null;
    },
    clear: function(): void {
        this.graphContainer = null;
        this.isolatedToggle = null;
        this.legenda = null;
    }
};

// Getters for module state
export function getSimulation(): ForceSimulation | null {
    return simulation;
}

export function getSvg(): SVGSelection | null {
    return svg;
}

export function getG(): GroupSelection | null {
    return g;
}

export function getZoom(): ZoomBehavior | null {
    return zoom;
}

export function getHiddenNodes(): Set<string> {
    return hiddenNodes;
}

export function getDomCache(): DOMCache {
    return domCache;
}

// Setters for module state
export function setSimulation(sim: ForceSimulation | null): void {
    simulation = sim;
}

export function setSvg(s: SVGSelection | null): void {
    svg = s;
}

export function setG(group: GroupSelection | null): void {
    g = group;
}

export function setZoom(z: ZoomBehavior | null): void {
    zoom = z;
}

// Clear all state
export function clearState(): void {
    if (simulation) {
        simulation.stop();
        simulation = null;
    }
    svg = null;
    g = null;
    zoom = null;
    domCache.clear();
}
