/**
 * "I was even thinking that it could be the WASM we ship"
 *
 * How long a call into WASM took, said to a sink. Nothing here knows Sentry:
 * wasm-metrics.ts binds the sink, so this runs without it. A call that throws
 * is still timed, and the error goes on to its caller.
 */

export type Module = 'ats' | 'laye';

export type Attributes = Record<string, string | number | boolean>;

export interface Sink {
    (module: Module, fn: string, ms: number, attributes: Attributes): void;
}

export interface Timer {
    timed<T>(module: Module, fn: string, run: () => T, attributes?: Attributes): T;
    timedAsync<T>(module: Module, fn: string, run: () => Promise<T>, attributes?: Attributes): Promise<T>;
}

export function timer(sink: Sink, now: () => number): Timer {
    return {
        timed(module, fn, run, attributes = {}) {
            const start = now();
            try {
                return run();
            } finally {
                sink(module, fn, now() - start, attributes);
            }
        },
        async timedAsync(module, fn, run, attributes = {}) {
            const start = now();
            try {
                return await run();
            } finally {
                sink(module, fn, now() - start, attributes);
            }
        },
    };
}
