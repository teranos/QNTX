/**
 * Spans of time, shown in the one grammar the ax language reads.
 *
 * "whatever the UI shows can be typed back into a query"
 *
 * The grammar and its arithmetic are Rust's (crates/ats/src/span.rs). This is
 * the showing side, kept here so a toast never waits on the wasm module, and
 * held to Rust by crates/ats/testdata/spans.json: span.test.ts checks every
 * vector there. `m` is minutes and `mo` months; a length with no start is
 * never called months. A readout is a span of two adjacent parts, `3h5m`, which
 * reads back as one compound span.
 */

export type Unit = 'millisecond' | 'second' | 'minute' | 'hour' | 'day' | 'week' | 'month' | 'year';

export interface Span {
    count: number;
    unit: Unit;
}

const SHORT: Record<Unit, string> = {
    millisecond: 'ms',
    second: 's',
    minute: 'm',
    hour: 'h',
    day: 'd',
    week: 'w',
    month: 'mo',
    year: 'y',
};

/** Fixed units, largest first, in milliseconds. Literals: the bundler
 *  loses a module const built from another (web/CLAUDE.md). */
const FIXED: Array<[Unit, number]> = [
    ['week', 604800000],
    ['day', 86400000],
    ['hour', 3600000],
    ['minute', 60000],
    ['second', 1000],
];

/** The fixed units a readout counts in, down to milliseconds. */
const READOUT: Array<[Unit, number]> = [
    ['week', 604800000],
    ['day', 86400000],
    ['hour', 3600000],
    ['minute', 60000],
    ['second', 1000],
    ['millisecond', 1],
];

/** The short form: `3d`, `2mo`. */
export function short(span: Span): string {
    return `${span.count}${SHORT[span.unit]}`;
}

/** The long form: `3 days`, `1 month`. */
export function long(span: Span): string {
    return span.count === 1 ? `1 ${span.unit}` : `${span.count} ${span.unit}s`;
}

/** A length with no start, in the largest whole fixed unit, weeks at most. */
export function spanOfMs(ms: number): Span {
    const length = Math.max(0, ms);
    for (const [unit, per] of FIXED) {
        if (length >= per) return { count: Math.floor(length / per), unit };
    }
    return { count: 0, unit: 'second' };
}

/** The time between two moments, in the largest whole unit, counted on the
 *  UTC calendar from the earlier: January 31 to the last of February is a
 *  month. */
export function spanBetween(aMs: number, bMs: number): Span {
    const from = Math.min(aMs, bMs);
    const to = Math.max(aMs, bMs);
    const months = monthsBetween(from, to);
    if (months >= 12) return { count: Math.floor(months / 12), unit: 'year' };
    if (months >= 1) return { count: months, unit: 'month' };
    return spanOfMs(to - from);
}

/** A length with no start as a two-unit readout: its largest whole fixed
 *  unit, then the unit right below it when that counts any, down to `finest`.
 *  `3h5m`, `3m12s`, `2d`, `500ms`; under `finest` is 0 of it. What Rust's
 *  Span::readout writes. */
export function readout(ms: number, finest: Unit): Span[] {
    const ladder = READOUT.slice(0, READOUT.findIndex(([unit]) => unit === finest) + 1);
    const parts: Span[] = [];
    let left = Math.max(0, ms);
    let shown = false;
    for (const [i, [unit, per]] of ladder.entries()) {
        const count = Math.floor(left / per);
        if (shown) {
            // The unit right below the first is the only other one shown.
            if (count >= 1) parts.push({ count, unit });
            break;
        }
        // Under the finest unit is 0 of it.
        if (count >= 1 || i === ladder.length - 1) {
            parts.push({ count, unit });
            left -= count * per;
            shown = true;
        }
    }
    return parts;
}

/** A readout's short form: `3h5m`. */
export function shortAll(parts: Span[]): string {
    return parts.map(short).join('');
}

/** A readout's long form: `3 hours 5 minutes`. */
export function longAll(parts: Span[]): string {
    return parts.map(long).join(' ');
}

/** `<span> ago` for a moment past, `in <span>` for one to come: both read
 *  back as the moment they describe. */
export function relative(thenMs: number, nowMs: number): string {
    const said = short(spanBetween(thenMs, nowMs));
    return thenMs > nowMs ? `in ${said}` : `${said} ago`;
}

/** Calendar months from `from` that land at or before `to`. */
function monthsBetween(from: number, to: number): number {
    const f = new Date(from);
    const t = new Date(to);
    const guess = (t.getUTCFullYear() - f.getUTCFullYear()) * 12 + (t.getUTCMonth() - f.getUTCMonth());
    return addMonths(from, guess) > to ? guess - 1 : guess;
}

/** Add calendar months, clamping the day to the target month's last day and
 *  keeping the time of day: what Rust's add_months does. */
function addMonths(ms: number, months: number): number {
    const d = new Date(ms);
    const index = d.getUTCFullYear() * 12 + d.getUTCMonth() + months;
    const year = Math.floor(index / 12);
    const month = index - year * 12;
    const target = new Date(0);
    target.setUTCFullYear(year, month + 1, 0);
    const lastDay = target.getUTCDate();
    target.setUTCFullYear(year, month, Math.min(d.getUTCDate(), lastDay));
    target.setUTCHours(d.getUTCHours(), d.getUTCMinutes(), d.getUTCSeconds(), d.getUTCMilliseconds());
    return target.getTime();
}
