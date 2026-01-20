/**
 * Tests for Type Attestation utilities
 *
 * Basic tests for node type state management
 */

import { describe, it, expect, beforeEach } from 'bun:test';
import { hiddenNodeTypes } from './type-attestations';

describe('Type Attestation State Management', () => {
  beforeEach(() => {
    hiddenNodeTypes.clear();
  });

  it('should track hidden node types', () => {
    hiddenNodeTypes.add('foo');
    expect(hiddenNodeTypes.has('foo')).toBe(true);
  });
});
