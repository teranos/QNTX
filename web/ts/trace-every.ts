/**
 * "Let's say per 10 min"
 *
 * A trace is sampled when at least `ms` have passed since the last one that was.
 * The first is always sampled. A sample rate is a chance per trace; this is a
 * spacing in time, which a chance cannot say.
 */
export function everyMs(ms: number, now: () => number): () => boolean {
    let last: number | null = null;
    return () => {
        const at = now();
        if (last !== null && at - last < ms) return false;
        last = at;
        return true;
    };
}
