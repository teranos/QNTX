import { describe, expect, test } from 'bun:test';
import { timer } from './wasm-timer';

// A call is timed by the clock it was given and said to the sink once, whether
// it returned or threw.

function fake() {
    const said: { module: string; fn: string; ms: number; attributes: Record<string, unknown> }[] = [];
    let t = 100;
    const { timed, timedAsync } = timer(
        (module, fn, ms, attributes) => said.push({ module, fn, ms, attributes }),
        () => t,
    );
    return { said, timed, timedAsync, advance: (ms: number) => { t += ms; } };
}

describe('Tim', () => {
    test('a call is said with its module, its name and how long it took', () => {
        const { said, timed, advance } = fake();
        const got = timed('ats', 'ax_filter', () => { advance(7); return 'rows'; });
        expect(got).toBe('rows');
        expect(said).toEqual([{ module: 'ats', fn: 'ax_filter', ms: 7, attributes: {} }]);
    });

    test('a call that waits is timed to when it was done', async () => {
        const { said, timedAsync, advance } = fake();
        const got = await timedAsync('laye', 'init', async () => { advance(40); return 1; }, { 'wasm.calls': 3 });
        expect(got).toBe(1);
        expect(said).toEqual([{ module: 'laye', fn: 'init', ms: 40, attributes: { 'wasm.calls': 3 } }]);
    });
});

describe('Spike', () => {
    test('a call that throws is still said, and its error reaches the caller', () => {
        const { said, timed, advance } = fake();
        expect(() => timed('ats', 'generate_asuid', () => { advance(2); throw new Error('no entropy'); })).toThrow('no entropy');
        expect(said).toHaveLength(1);
        expect(said[0]?.ms).toBe(2);
    });

    test('a call that rejects is still said, and its rejection reaches the caller', async () => {
        const { said, timedAsync, advance } = fake();
        await expect(timedAsync('ats', 'init_store', async () => { advance(5); throw new Error('idb refused'); })).rejects.toThrow('idb refused');
        expect(said).toHaveLength(1);
        expect(said[0]?.ms).toBe(5);
    });
});
