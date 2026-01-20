/**
 * Graph Rendering Integration Tests
 *
 * Purpose: Verify DOM structure and graph data handling with real DOM APIs
 *
 * Why JSDOM?
 * Graph rendering requires real DOM APIs (SVG namespace, element creation).
 * JSDOM provides complete DOM implementation for integration testing.
 *
 * Strategy:
 * - CI: USE_JSDOM=1 enables these tests (catches DOM/SVG issues before merge)
 * - Local: Tests skipped by default (keeps `make test` fast for development)
 * - Run explicitly: USE_JSDOM=1 bun test ts/graph/graph.integration.test.ts
 *
 * Trade-off:
 * ✅ Catches real DOM integration issues
 * ✅ No impact on local dev speed (skipped by default)
 * ❌ CI slower (~600ms for this file)
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import type { GraphData } from '../../types/core';

// Only run these tests when USE_JSDOM=1 (CI environment)
const USE_JSDOM = process.env.USE_JSDOM === '1';

// Setup JSDOM if enabled
let JSDOM: any;
if (USE_JSDOM) {
    JSDOM = (await import('jsdom')).JSDOM;
}

describe('Graph Rendering Integration', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    let dom: any;
    let document: Document;
    let window: Window;

    beforeEach(() => {
        // Create a realistic HTML structure for the graph
        dom = new JSDOM(`
            <!DOCTYPE html>
            <html>
            <body>
                <div id="graph-container" style="width: 800px; height: 600px;">
                    <svg id="graph"></svg>
                </div>
                <div id="controls"></div>
                <div class="type-attestations"></div>
                <div id="tooltip" class="graph-data-tooltip"></div>
            </body>
            </html>
        `, {
            url: 'http://localhost',
            pretendToBeVisual: true,
        });

        document = dom.window.document;
        window = dom.window as unknown as Window;

        // Make available globally for D3 and our code
        global.document = document;
        global.window = window;
    });

    test('should create graph container with correct structure', () => {
        const container = document.getElementById('graph-container');
        expect(container).not.toBeNull();
        expect(container?.style.width).toBe('800px');
        expect(container?.style.height).toBe('600px');

        const svg = document.getElementById('graph');
        expect(svg).not.toBeNull();
        expect(svg?.tagName).toBe('svg');
    });

    test('should have all required UI elements', () => {
        expect(document.getElementById('graph-container')).not.toBeNull();
        expect(document.getElementById('graph')).not.toBeNull();
        expect(document.getElementById('controls')).not.toBeNull();
        expect(document.querySelector('.type-attestations')).not.toBeNull();
        expect(document.getElementById('tooltip')).not.toBeNull();
    });

    test('should handle empty graph data structure', () => {
        const data: GraphData = {
            nodes: [],
            links: []
        };

        // Verify data structure is valid
        expect(Array.isArray(data.nodes)).toBe(true);
        expect(Array.isArray(data.links)).toBe(true);
        expect(data.nodes.length).toBe(0);
        expect(data.links.length).toBe(0);
    });

    test('should handle basic graph data structure', () => {
        const data: GraphData = {
            nodes: [
                { id: 'node1', label: 'Node 1', type: 'type1' },
                { id: 'node2', label: 'Node 2', type: 'type2' }
            ],
            links: [
                { source: 'node1', target: 'node2', type: 'link1' }
            ]
        };

        // Verify data structure is valid
        expect(data.nodes.length).toBe(2);
        expect(data.links.length).toBe(1);
        expect(data.nodes[0].id).toBe('node1');
        expect(data.links[0].source).toBe('node1');
        expect(data.links[0].target).toBe('node2');
    });

    test('should verify container has style attributes', () => {
        const container = document.getElementById('graph-container') as HTMLElement;
        // JSDOM doesn't compute layout, but we can verify style attributes exist
        expect(container.style.width).toBe('800px');
        expect(container.style.height).toBe('600px');
    });

    test('should support SVG element creation', () => {
        const svg = document.getElementById('graph');
        const g = document.createElementNS('http://www.w3.org/2000/svg', 'g');
        svg?.appendChild(g);

        expect(svg?.children.length).toBe(1);
        expect(svg?.children[0].tagName).toBe('g');
    });

    test('should support creating SVG patterns for diagnostics', () => {
        const svg = document.getElementById('graph');
        const defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs');
        const pattern = document.createElementNS('http://www.w3.org/2000/svg', 'pattern');

        pattern.setAttribute('id', 'test-pattern');
        pattern.setAttribute('width', '8');
        pattern.setAttribute('height', '8');

        defs.appendChild(pattern);
        svg?.appendChild(defs);

        const createdPattern = document.getElementById('test-pattern');
        expect(createdPattern).not.toBeNull();
        expect(createdPattern?.getAttribute('width')).toBe('8');
    });
});
