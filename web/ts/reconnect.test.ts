import { test, expect } from 'bun:test';
import { delayAfterAttempt, connectingLabel, humanElapsed, ladderStated, SAY_AFTER_MS } from './reconnect';

// 3×2s, 3×5s, 3×10s, 3×30s, 4×1m, 4×2m, then 5m indefinitely.
test('the ladder is the ladder', () => {
    const seconds = [];
    for (let attempt = 1; attempt <= 22; attempt++) {
        seconds.push(delayAfterAttempt(attempt) / 1000);
    }
    expect(seconds).toEqual([
        2, 2, 2,
        5, 5, 5,
        10, 10, 10,
        30, 30, 30,
        60, 60, 60, 60,
        120, 120, 120, 120,
        300, 300,
    ]);
});

test('past the ladder it stays at five minutes', () => {
    expect(delayAfterAttempt(500) / 1000).toBe(300);
});

test('the ladder can be read without running it', () => {
    expect(ladderStated()).toBe('3×2s, 3×5s, 3×10s, 3×30s, 4×1m, 4×2m, then every 5m');
});

// A blip is not news. Under five minutes the chip says only that it is trying.
test('a short outage says nothing but connecting', () => {
    expect(connectingLabel(4, 0, SAY_AFTER_MS - 1)).toBe('Connecting...');
});

test('past five minutes it says how long and how many', () => {
    expect(connectingLabel(12, 0, 5 * 60_000)).toBe('Connecting 5m · 12 attempts');
});

test('one attempt is not one attempts', () => {
    expect(connectingLabel(1, 0, 6 * 60_000)).toBe('Connecting 6m · 1 attempt');
});

test('an hour reads as an hour', () => {
    expect(humanElapsed(60 * 60_000)).toBe('1h');
    expect(humanElapsed(63 * 60_000)).toBe('1h 3m');
    expect(humanElapsed(45_000)).toBe('45s');
    expect(humanElapsed(9 * 60_000)).toBe('9m');
});

// The count and the clock are both counted, not estimated — a label that
// guessed either would be the same failure as everything else tonight.
test('a long outage states both, and neither is rounded away', () => {
    expect(connectingLabel(37, 0, 2 * 60 * 60_000 + 7 * 60_000)).toBe('Connecting 2h 7m · 37 attempts');
});
