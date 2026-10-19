import { describe, expect, test } from 'bun:test';
import { everyMs } from './trace-every';

// A trace is sampled once per spacing: the first, then the first to come after
// the spacing has passed.

describe('Tim', () => {
    test('the first trace is sampled, and the next only after the spacing', () => {
        let t = 1000;
        const sample = everyMs(600000, () => t);
        expect(sample()).toBe(true);
        t += 599999;
        expect(sample()).toBe(false);
        t += 1;
        expect(sample()).toBe(true);
    });

    test('the spacing counts from the last trace that was sampled, not the last asked', () => {
        let t = 0;
        const sample = everyMs(600000, () => t);
        expect(sample()).toBe(true);
        t = 300000;
        expect(sample()).toBe(false);
        t = 600000;
        expect(sample()).toBe(true);
    });
});

describe('Spike', () => {
    test('a spacing of zero samples every trace', () => {
        const sample = everyMs(0, () => 5);
        expect(sample()).toBe(true);
        expect(sample()).toBe(true);
    });
});
