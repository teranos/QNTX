/**
 * Tests for Pulse type utilities
 *
 * Covers interval formatting/parsing and type validation
 */

import { describe, it, expect } from 'bun:test';
import { formatInterval, parseInterval } from './types';

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
      expect(formatInterval(604800)).toBe('7d');
      expect(formatInterval(2592000)).toBe('30d');
    });

    it('should handle edge cases', () => {
      expect(formatInterval(61)).toBe('1m'); // Rounds down
      expect(formatInterval(3661)).toBe('1h'); // Rounds down
      expect(formatInterval(86461)).toBe('1d'); // Rounds down
    });

    it('should handle very large values', () => {
      expect(formatInterval(31536000)).toBe('365d'); // 1 year
      expect(formatInterval(Number.MAX_SAFE_INTEGER)).toMatch(/\d+d/);
    });
  });

  describe('parseInterval', () => {
    it('should parse seconds', () => {
      expect(parseInterval('1s')).toBe(1);
      expect(parseInterval('30s')).toBe(30);
      expect(parseInterval('60s')).toBe(60);
    });

    it('should parse minutes', () => {
      expect(parseInterval('1m')).toBe(60);
      expect(parseInterval('15m')).toBe(900);
      expect(parseInterval('30m')).toBe(1800);
      expect(parseInterval('60m')).toBe(3600);
    });

    it('should parse hours', () => {
      expect(parseInterval('1h')).toBe(3600);
      expect(parseInterval('6h')).toBe(21600);
      expect(parseInterval('12h')).toBe(43200);
      expect(parseInterval('24h')).toBe(86400);
    });

    it('should parse days', () => {
      expect(parseInterval('1d')).toBe(86400);
      expect(parseInterval('7d')).toBe(604800);
      expect(parseInterval('30d')).toBe(2592000);
    });

    it('should handle whitespace', () => {
      expect(parseInterval('1 s')).toBe(1);
      expect(parseInterval('30 m')).toBe(1800);
      expect(parseInterval('6 h')).toBe(21600);
      expect(parseInterval('1 d')).toBe(86400);
    });

    it('should return null for invalid format', () => {
      expect(parseInterval('')).toBeNull();
      expect(parseInterval('abc')).toBeNull();
      expect(parseInterval('123')).toBeNull(); // Missing unit
      expect(parseInterval('s')).toBeNull(); // Missing number
      expect(parseInterval('1x')).toBeNull(); // Invalid unit
      expect(parseInterval('1.5h')).toBeNull(); // Decimals not supported
      expect(parseInterval('-5m')).toBeNull(); // Negative not supported
    });

    it('should handle zero', () => {
      expect(parseInterval('0s')).toBe(0);
      expect(parseInterval('0m')).toBe(0);
      expect(parseInterval('0h')).toBe(0);
      expect(parseInterval('0d')).toBe(0);
    });

    it('should handle large values', () => {
      expect(parseInterval('365d')).toBe(31536000); // 1 year
      expect(parseInterval('999d')).toBe(86313600);
    });
  });

  describe('interval round-trip', () => {
    it('should maintain exact values for common intervals', () => {
      const intervals = [
        { seconds: 60, formatted: '1m' },
        { seconds: 900, formatted: '15m' },
        { seconds: 1800, formatted: '30m' },
        { seconds: 3600, formatted: '1h' },
        { seconds: 21600, formatted: '6h' },
        { seconds: 43200, formatted: '12h' },
        { seconds: 86400, formatted: '1d' },
      ];

      intervals.forEach(({ seconds, formatted }) => {
        expect(formatInterval(seconds)).toBe(formatted);
        expect(parseInterval(formatted)).toBe(seconds);
      });
    });

    it('should round-trip for all units', () => {
      expect(parseInterval(formatInterval(30))).toBe(30); // 30s
      expect(parseInterval(formatInterval(1800))).toBe(1800); // 30m
      expect(parseInterval(formatInterval(21600))).toBe(21600); // 6h
      expect(parseInterval(formatInterval(604800))).toBe(604800); // 7d
    });
  });
});
