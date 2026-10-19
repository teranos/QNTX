/**
 * "how do you make it less specific to the door"
 *
 * What a loop costs, said and not fixed: a requestAnimationFrame loop, an
 * interval, anything that keeps waking the page. It counts what the loop does
 * and says it when the loop stops, when the page is hidden and when it is left,
 * so a loop that is never stopped still says it is alive, and for how long.
 * Nothing here knows Sentry or the DOM: loop-metrics.ts binds the sink.
 */

export type Attributes = Record<string, string | number | boolean>;

export interface Sink {
    (name: string, value: number, unit: string | undefined, attributes: Attributes): void;
}

/** Why a report was made: the loop was stopped, or the page was hidden or left. */
export type Reason = 'stop' | 'hidden' | 'pagehide';

export interface Meter {
    /** The loop woke, whether or not it did its work. */
    called(): void;
    /**
     * The loop did its work, and how long that took on this thread. `seen` is
     * whether what it draws was on the page; a loop that draws nothing leaves it out.
     */
    ran(ms: number, seen?: boolean): void;
    report(reason: Reason): void;
}

type Key = 'seen' | 'unseen' | 'unsaid';

const KEYS: readonly Key[] = ['seen', 'unseen', 'unsaid'];

/** One loop's meter. `where` is whatever the loop itself knows about where it runs. */
export function meter(name: string, sink: Sink, now: () => number, where: () => Attributes): Meter {
    let started = false;
    let since = now();
    let callbacks = 0;
    const runs: Record<Key, number> = { seen: 0, unseen: 0, unsaid: 0 };
    const cpu: Record<Key, number> = { seen: 0, unseen: 0, unsaid: 0 };

    const attributesOf = (more: Attributes): Attributes => ({ ...where(), 'loop.name': name, ...more });

    return {
        called() {
            callbacks += 1;
        },
        ran(ms, seen) {
            if (!started) {
                started = true;
                sink('loop.started', 1, undefined, attributesOf({}));
            }
            const key: Key = seen === undefined ? 'unsaid' : seen ? 'seen' : 'unseen';
            runs[key] += 1;
            cpu[key] += ms;
        },
        report(reason) {
            const at = now();
            const attributes = attributesOf({ 'loop.reason': reason });
            sink('loop.alive', at - since, 'millisecond', attributes);
            sink('loop.callbacks', callbacks, undefined, attributes);
            for (const key of KEYS) {
                if (runs[key] === 0) continue;
                const kept = key === 'unsaid' ? attributes : { ...attributes, 'loop.seen': key === 'seen' };
                sink('loop.runs', runs[key], undefined, kept);
                sink('loop.cpu', cpu[key], 'millisecond', kept);
            }
            if (reason === 'stop') sink('loop.stopped', 1, undefined, attributes);
            since = at;
            callbacks = 0;
            for (const key of KEYS) {
                runs[key] = 0;
                cpu[key] = 0;
            }
        },
    };
}

/**
 * Every loop that is metered and not yet stopped. Asking it to report says,
 * for each one still standing, that it is: which is how a loop that nobody
 * stopped gets named.
 */
export interface Loops {
    start(name: string, where?: () => Attributes): Meter;
    /** Every loop still alive reports, and none is dropped: the page may come back. */
    reportAlive(reason: Reason): void;
}

export function loops(sink: Sink, now: () => number): Loops {
    const alive = new Set<Meter>();
    return {
        start(name, where = () => ({})) {
            const inner = meter(name, sink, now, where);
            const outer: Meter = {
                called: () => inner.called(),
                ran: (ms, seen) => inner.ran(ms, seen),
                report(reason) {
                    inner.report(reason);
                    if (reason === 'stop') alive.delete(outer);
                },
            };
            alive.add(outer);
            return outer;
        },
        reportAlive(reason) {
            for (const each of alive) each.report(reason);
        },
    };
}
