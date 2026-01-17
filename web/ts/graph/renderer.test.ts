/**
 * Tests for Graph Renderer utilities
 *
 * Critical tests for physics configuration and node visibility filtering
 */

import { describe, it, expect } from 'bun:test';
import { filterVisibleNodes } from './renderer';
import { getLinkDistance, getLinkStrength } from './physics';
import { GRAPH_PHYSICS } from '../config';

describe('Graph Visibility', () => {
  it('should filter visible nodes', () => {
    const nodes = [
      { id: 'foo', visible: true },
      { id: 'bar', visible: false },
      { id: 'baz' } // no visible property = visible
    ];
    const result = filterVisibleNodes(nodes);
    expect(result).toHaveLength(2);
    expect(result.map(n => n.id)).toContain('foo');
    expect(result.map(n => n.id)).toContain('baz');
  });
});

describe('Link Physics Configuration', () => {
  describe('getLinkDistance', () => {
    it('should use metadata when available', () => {
      const link = { type: 'is_child_of', source: 'a', target: 'b' };
      const metadata = [
        { type: 'is_child_of', label: 'Child Of', link_distance: 42, link_strength: 0.5, count: 10 }
      ];
      expect(getLinkDistance(link as any, metadata)).toBe(42);
    });

    it('should fall back to default without metadata', () => {
      const link = { type: 'is_child_of', source: 'a', target: 'b' };
      expect(getLinkDistance(link as any)).toBe(GRAPH_PHYSICS.LINK_DISTANCE);
    });

    it('should fall back to default when metadata missing type', () => {
      const link = { type: 'is_child_of', source: 'a', target: 'b' };
      const metadata = [
        { type: 'points_to', label: 'Points To', link_distance: 60, link_strength: 0.2, count: 5 }
      ];
      expect(getLinkDistance(link as any, metadata)).toBe(GRAPH_PHYSICS.LINK_DISTANCE);
    });

    it('should return default distance for non-git link types', () => {
      const link = { type: 'related_to', source: 'a', target: 'b' };
      expect(getLinkDistance(link as any)).toBe(GRAPH_PHYSICS.LINK_DISTANCE);
    });

    it('should return default distance for unknown link types', () => {
      const link = { type: 'custom_type', source: 'a', target: 'b' };
      expect(getLinkDistance(link as any)).toBe(GRAPH_PHYSICS.LINK_DISTANCE);
    });

    it('should return default distance for undefined type', () => {
      const link = { source: 'a', target: 'b' };
      expect(getLinkDistance(link as any)).toBe(GRAPH_PHYSICS.LINK_DISTANCE);
    });
  });

  describe('getLinkStrength', () => {
    it('should use metadata when available', () => {
      const link = { type: 'points_to', source: 'a', target: 'b' };
      const metadata = [
        { type: 'points_to', label: 'Points To', link_distance: 60, link_strength: 0.8, count: 5 }
      ];
      expect(getLinkStrength(link as any, metadata)).toBe(0.8);
    });

    it('should fall back to default without metadata', () => {
      const link = { type: 'is_child_of', source: 'a', target: 'b' };
      expect(getLinkStrength(link as any)).toBe(GRAPH_PHYSICS.DEFAULT_LINK_STRENGTH);
    });

    it('should fall back to default when metadata missing type', () => {
      const link = { type: 'points_to', source: 'a', target: 'b' };
      const metadata = [
        { type: 'is_child_of', label: 'Child Of', link_distance: 50, link_strength: 0.3, count: 10 }
      ];
      expect(getLinkStrength(link as any, metadata)).toBe(GRAPH_PHYSICS.DEFAULT_LINK_STRENGTH);
    });

    it('should return default strength for non-git link types', () => {
      const link = { type: 'related_to', source: 'a', target: 'b' };
      expect(getLinkStrength(link as any)).toBe(GRAPH_PHYSICS.DEFAULT_LINK_STRENGTH);
    });

    it('should return default strength for unknown link types', () => {
      const link = { type: 'custom_type', source: 'a', target: 'b' };
      expect(getLinkStrength(link as any)).toBe(GRAPH_PHYSICS.DEFAULT_LINK_STRENGTH);
    });

    it('should return default strength for undefined type', () => {
      const link = { source: 'a', target: 'b' };
      expect(getLinkStrength(link as any)).toBe(GRAPH_PHYSICS.DEFAULT_LINK_STRENGTH);
    });
  });

});

// Isolated Node Detection (from renderer.ts lines 78-94)
import type { GraphData, Node } from '../../types/core';

function detectIsolatedNodes(data: GraphData): Set<string> {
    const nodeHasLinks = new Set<string>();
    data.links.forEach(link => {
        const sourceId = typeof link.source === 'object' ? (link.source as Node).id : link.source as string;
        const targetId = typeof link.target === 'object' ? (link.target as Node).id : link.target as string;
        nodeHasLinks.add(sourceId);
        nodeHasLinks.add(targetId);
    });
    return nodeHasLinks;
}

describe('Isolated Nodes', () => {
    it('should detect nodes with string IDs', () => {
        const data: GraphData = {
            nodes: [
                { id: 'a', label: 'A', type: 't' },
                { id: 'b', label: 'B', type: 't' },
                { id: 'c', label: 'C', type: 't' }
            ],
            links: [{ source: 'a', target: 'b', type: 'link' }]
        };

        const connected = detectIsolatedNodes(data);
        expect(connected.has('a')).toBe(true);
        expect(connected.has('b')).toBe(true);
        expect(connected.has('c')).toBe(false); // isolated
    });

    it('should detect nodes with object references', () => {
        const a = { id: 'a', label: 'A', type: 't' };
        const b = { id: 'b', label: 'B', type: 't' };
        const c = { id: 'c', label: 'C', type: 't' };

        const data: GraphData = {
            nodes: [a, b, c],
            links: [{ source: a, target: b, type: 'link' }]
        };

        const connected = detectIsolatedNodes(data);
        expect(connected.has('a')).toBe(true);
        expect(connected.has('b')).toBe(true);
        expect(connected.has('c')).toBe(false); // isolated
    });

    it('should handle empty graph', () => {
        const data: GraphData = { nodes: [], links: [] };
        const connected = detectIsolatedNodes(data);
        expect(connected.size).toBe(0);
    });

    it('should handle self-loops', () => {
        const data: GraphData = {
            nodes: [
                { id: 'a', label: 'A', type: 't' },
                { id: 'b', label: 'B', type: 't' }
            ],
            links: [{ source: 'a', target: 'a', type: 'self' }]
        };

        const connected = detectIsolatedNodes(data);
        expect(connected.has('a')).toBe(true); // self-connected
        expect(connected.has('b')).toBe(false); // isolated
    });
});
