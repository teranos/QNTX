/**
 * Tests for Pulse type utilities
 *
 * Covers interval formatting/parsing and type validation
 */

import { describe, it, expect } from 'bun:test';
import { formatInterval } from './types';

describe('Pulse Type Utilities', () => {
  describe('formatInterval', () => {
    it('should format seconds', () => {
      expect(formatInterval(0)).toBe('0s');
      expect(formatInterval(1)).toBe('1s');
      expect(formatInterval(30)).toBe('30s');
      expect(formatInterval(59)).toBe('59s');
    });

    it('should format minutes', () => {
      expect(formatInterval(60)).toBe('1m');
      expect(formatInterval(120)).toBe('2m');
      expect(formatInterval(1800)).toBe('30m');
      expect(formatInterval(3599)).toBe('59m');
    });

    it('should format hours', () => {
      expect(formatInterval(3600)).toBe('1h');
      expect(formatInterval(7200)).toBe('2h');
      expect(formatInterval(21600)).toBe('6h');
      expect(formatInterval(43200)).toBe('12h');
      expect(formatInterval(86399)).toBe('23h');
    });

    it('should format days', () => {
      expect(formatInterval(86400)).toBe('1d');
      expect(formatInterval(172800)).toBe('2d');
      expect(formatInterval(604800)).toBe('1w');
      expect(formatInterval(2592000)).toBe('4w');
    });

    it('should handle edge cases', () => {
      expect(formatInterval(61)).toBe('1m'); // Rounds down
      expect(formatInterval(3661)).toBe('1h'); // Rounds down
      expect(formatInterval(86461)).toBe('1d'); // Rounds down
    });

    it('should handle very large values', () => {
      expect(formatInterval(31536000)).toBe('52w'); // a length has no calendar: weeks at most
      expect(formatInterval(Number.MAX_SAFE_INTEGER)).toMatch(/\d+w/);
    });
  });


  // What formatInterval shows reads back: the span grammar's round trip is
  // held in Rust (crates/ats/src/span.rs, what_is_shown_reads_back), and
  // span.test.ts holds this side to it.
});
