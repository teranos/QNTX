/**
 * Tests for Graph Renderer utilities
 *
 * Critical tests for physics configuration and node visibility filtering
 */

import { describe, it, expect } from 'bun:test';
import { filterVisibleNodes } from './graph-renderer';
import { getLinkDistance, getLinkStrength } from './graph-physics';
import { GRAPH_PHYSICS } from './config';

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
    it('should return git child distance for is_child_of links', () => {
      const link = { type: 'is_child_of', source: 'a', target: 'b' };
      expect(getLinkDistance(link as any)).toBe(GRAPH_PHYSICS.GIT_CHILD_LINK_DISTANCE);
    });

    it('should return git branch distance for points_to links', () => {
      const link = { type: 'points_to', source: 'a', target: 'b' };
      expect(getLinkDistance(link as any)).toBe(GRAPH_PHYSICS.GIT_BRANCH_LINK_DISTANCE);
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
    it('should return git child strength for is_child_of links', () => {
      const link = { type: 'is_child_of', source: 'a', target: 'b' };
      expect(getLinkStrength(link as any)).toBe(GRAPH_PHYSICS.GIT_CHILD_LINK_STRENGTH);
    });

    it('should return git branch strength for points_to links', () => {
      const link = { type: 'points_to', source: 'a', target: 'b' };
      expect(getLinkStrength(link as any)).toBe(GRAPH_PHYSICS.GIT_BRANCH_LINK_STRENGTH);
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

  describe('Physics values sanity checks', () => {
    it('git child distance should be less than default', () => {
      expect(GRAPH_PHYSICS.GIT_CHILD_LINK_DISTANCE).toBeLessThan(GRAPH_PHYSICS.LINK_DISTANCE);
    });

    it('git branch distance should be less than default', () => {
      expect(GRAPH_PHYSICS.GIT_BRANCH_LINK_DISTANCE).toBeLessThan(GRAPH_PHYSICS.LINK_DISTANCE);
    });

    it('git child strength should be less than default', () => {
      expect(GRAPH_PHYSICS.GIT_CHILD_LINK_STRENGTH).toBeLessThan(GRAPH_PHYSICS.DEFAULT_LINK_STRENGTH);
    });

    it('git branch strength should be less than default', () => {
      expect(GRAPH_PHYSICS.GIT_BRANCH_LINK_STRENGTH).toBeLessThan(GRAPH_PHYSICS.DEFAULT_LINK_STRENGTH);
    });
  });
});
