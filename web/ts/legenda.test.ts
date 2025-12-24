/**
 * Tests for Legenda utilities
 *
 * Basic tests for node type state management
 */

import { describe, it, expect, beforeEach } from 'vitest';
import { hiddenNodeTypes } from './legenda';

describe('Legenda State Management', () => {
  beforeEach(() => {
    hiddenNodeTypes.clear();
  });

  it('should track hidden node types', () => {
    hiddenNodeTypes.add('foo');
    expect(hiddenNodeTypes.has('foo')).toBe(true);
  });
});
