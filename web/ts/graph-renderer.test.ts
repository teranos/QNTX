/**
 * Tests for Graph Renderer utilities
 *
 * Basic tests for node visibility filtering
 */

import { describe, it, expect } from 'vitest';
import { filterVisibleNodes } from './graph-renderer';

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
