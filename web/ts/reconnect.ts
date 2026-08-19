// How long to wait after a failed attempt, and what the chip is allowed to say
// about it. Both are pure so the ladder can be read rather than inferred from a
// running timer.

interface Rung {
    times: number;
    delayMs: number;
}

// 3×2s, 3×5s, 3×10s, 3×30s, 4×1m, 4×2m, then 5m for as long as it takes.
const LADDER: Rung[] = [
    { times: 3, delayMs: 2_000 },
    { times: 3, delayMs: 5_000 },
    { times: 3, delayMs: 10_000 },
    { times: 3, delayMs: 30_000 },
    { times: 4, delayMs: 60_000 },
    { times: 4, delayMs: 120_000 },
];

const FOREVER_MS = 300_000;

// Below this the chip says nothing but "Connecting" — a blip is not news.
export const SAY_AFTER_MS = 300_000;

// attempt is 1-based and counts attempts already made.
export function delayAfterAttempt(attempt: number): number {
    let remaining = attempt;
    for (const rung of LADDER) {
        if (remaining <= rung.times) return rung.delayMs;
        remaining -= rung.times;
    }
    return FOREVER_MS;
}

// The whole ladder as one line, so nobody has to run it to know it.
export function ladderStated(): string {
    const rungs = LADDER.map(r => `${r.times}×${humanDelay(r.delayMs)}`);
    return `${rungs.join(', ')}, then every ${humanDelay(FOREVER_MS)}`;
}

function humanDelay(ms: number): string {
    if (ms < 60_000) return `${Math.round(ms / 1000)}s`;
    return `${Math.round(ms / 60_000)}m`;
}

export function humanElapsed(ms: number): string {
    const seconds = Math.floor(ms / 1000);
    if (seconds < 60) return `${seconds}s`;

    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return `${minutes}m`;

    const hours = Math.floor(minutes / 60);
    const spare = minutes - hours * 60;
    return spare === 0 ? `${hours}h` : `${hours}h ${spare}m`;
}

// What the chip says. Short outages read as "Connecting" and nothing more;
// past SAY_AFTER_MS it states how long and how many times, both counted.
export function connectingLabel(attempts: number, sinceMs: number, nowMs: number): string {
    const elapsed = nowMs - sinceMs;
    if (elapsed < SAY_AFTER_MS) return 'Connecting...';

    const tries = attempts === 1 ? '1 attempt' : `${attempts} attempts`;
    return `Connecting ${humanElapsed(elapsed)} · ${tries}`;
}
