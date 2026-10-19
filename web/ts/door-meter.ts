/**
 * "The suspicion that it keeps persisting on the background."
 *
 * What the door's field costs, said and not fixed. It counts what the loop
 * does and reports it when the door stops, when the page is hidden and when it
 * is left, so a field that is never stopped still says how long it lived.
 * Nothing here knows Sentry or WebGL: door-metrics.ts binds the sink.
 */

export type Attributes = Record<string, string | number | boolean>;

export interface Sink {
    (name: string, value: number, unit: string | undefined, attributes: Attributes): void;
}

/** Why a report was made: the door was stopped, or the page was hidden or left. */
export type Reason = 'stop' | 'hidden' | 'pagehide';

export interface Meter {
    /** The loop's callback ran, whether or not it drew. */
    called(): void;
    /** A frame was drawn, and how long it took to hand to the GPU. `seen` is whether the canvas was on the page. */
    drew(ms: number, seen: boolean): void;
    report(reason: Reason): void;
}

export function meter(sink: Sink, now: () => number, where: () => Attributes): Meter {
    let started = false;
    let since = now();
    let callbacks = 0;
    const frames = { seen: 0, unseen: 0 };
    const cpu = { seen: 0, unseen: 0 };

    return {
        called() {
            callbacks += 1;
        },
        drew(ms, seen) {
            if (!started) {
                started = true;
                sink('door.field.started', 1, undefined, where());
            }
            const key = seen ? 'seen' : 'unseen';
            frames[key] += 1;
            cpu[key] += ms;
        },
        report(reason) {
            const at = now();
            const attributes = { ...where(), 'door.reason': reason };
            sink('door.field.alive', at - since, 'millisecond', attributes);
            sink('door.field.callbacks', callbacks, undefined, attributes);
            for (const key of ['seen', 'unseen'] as const) {
                if (frames[key] === 0) continue;
                const kept = { ...attributes, 'door.seen': key === 'seen' };
                sink('door.field.frames', frames[key], undefined, kept);
                sink('door.field.cpu', cpu[key], 'millisecond', kept);
            }
            if (reason === 'stop') sink('door.field.stopped', 1, undefined, attributes);
            since = at;
            callbacks = 0;
            frames.seen = 0;
            frames.unseen = 0;
            cpu.seen = 0;
            cpu.unseen = 0;
        },
    };
}
