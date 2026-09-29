/**
 * Sparklines: named things over time, read down, one line each.
 *
 * "The axis of time is more useful than a tally." Each row is a name, how often
 * it was seen in each bucket of one shared window, and when it was last seen. No
 * count is shown and rows are ordered by when they were last seen, not by how
 * often.
 *
 * Staands is Umami, so the buckets are Umami's: the unit comes from how long the
 * window is (getMinimumUnit, umami v3.3.1 src/lib/date.ts) and the last-seen
 * label is written in that unit's format (DATE_FORMATS there).
 *
 * The caller decides what a name is — text, or something you can press.
 */

const MUTE = 'var(--text-on-dark-tertiary)';
const LINE = 'var(--border-on-dark)';

/** Draw one series as an 80×16 line, scaled to its own maximum. */
export function renderSparkline(data: (number | null)[]): string {
    const values = data.filter((v): v is number => v != null);
    if (values.length < 2) return '';

    const w = 80;
    const h = 16;
    const max = Math.max(...values);
    if (max === 0) return '';

    const points = data.map((v, i) => {
        if (v == null) return null;
        const x = (i / (data.length - 1)) * w;
        const y = h - (v / max) * (h - 2) - 1;
        return `${x},${y}`;
    }).filter(Boolean);

    if (points.length < 2) return '';

    return `<svg viewBox="0 0 ${w} ${h}" style="width: ${w}px; height: ${h}px;">
        <polyline points="${points.join(' ')}" fill="none" stroke="#64748b" stroke-width="1" />
    </svg>`;
}

/** Umami's units. */
export type Unit = 'minute' | 'hour' | 'day' | 'month' | 'year';

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;

function calendarMonthsBetween(start: number, end: number): number {
    const a = new Date(start);
    const b = new Date(end);
    return (b.getFullYear() - a.getFullYear()) * 12 + (b.getMonth() - a.getMonth());
}

/** The unit Umami groups a date range by: getMinimumUnit(start, end, true). */
export function unitFor(start: number, end: number): Unit {
    if (Math.trunc((end - start) / MINUTE) <= 60) return 'minute';
    if (Math.trunc((end - start) / HOUR) <= 48) return 'hour';
    if (calendarMonthsBetween(start, end) <= 7) return 'day';
    if (calendarMonthsBetween(start, end) <= 24) return 'month';
    return 'year';
}

/** The start of the bucket a moment falls in, in local time. */
export function bucketStart(t: number, unit: Unit): number {
    const d = new Date(t);
    switch (unit) {
        case 'minute': d.setSeconds(0, 0); break;
        case 'hour': d.setMinutes(0, 0, 0); break;
        case 'day': d.setHours(0, 0, 0, 0); break;
        case 'month': d.setDate(1); d.setHours(0, 0, 0, 0); break;
        case 'year': d.setMonth(0, 1); d.setHours(0, 0, 0, 0); break;
    }
    return d.getTime();
}

function nextBucket(t: number, unit: Unit): number {
    const d = new Date(t);
    switch (unit) {
        case 'minute': d.setMinutes(d.getMinutes() + 1); break;
        case 'hour': d.setHours(d.getHours() + 1); break;
        case 'day': d.setDate(d.getDate() + 1); break;
        case 'month': d.setMonth(d.getMonth() + 1); break;
        case 'year': d.setFullYear(d.getFullYear() + 1); break;
    }
    return d.getTime();
}

/** One shared window: every row in it is bucketed the same way. */
export interface Window {
    start: number;
    end: number;
    unit: Unit;
}

/** The window from the earliest moment to now, in Umami's unit for its length. */
export function windowOf(times: number[], now: number = Date.now()): Window {
    const start = times.length > 0 ? Math.min(...times) : now;
    return { start, end: now, unit: unitFor(start, now) };
}

/** How many moments fall in each bucket of the window, empty buckets at zero. */
export function seriesOf(times: number[], w: Window): number[] {
    const starts: number[] = [];
    for (let t = bucketStart(w.start, w.unit); t <= w.end; t = nextBucket(t, w.unit)) {
        starts.push(t);
    }
    const counts = starts.map(() => 0);
    for (const t of times) {
        const b = bucketStart(t, w.unit);
        const i = starts.indexOf(b);
        if (i >= 0) counts[i]++;
    }
    return counts;
}

const pad = (n: number): string => String(n).padStart(2, '0');

/** A moment in its unit's format: Umami's DATE_FORMATS. */
export function formatIn(t: number, unit: Unit): string {
    const d = new Date(t);
    const day = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
    switch (unit) {
        case 'minute': return `${day} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
        case 'hour': return `${day} ${pad(d.getHours())}`;
        case 'day': return day;
        case 'month': return `${d.getFullYear()}-${pad(d.getMonth() + 1)}`;
        case 'year': return String(d.getFullYear());
    }
}

/** A name and every moment it was seen. */
export interface Seen {
    name: string;
    times: number[];
}

/** Most recently seen first; a tie reads the same way twice, by name. */
export function byLastSeen(rows: Seen[]): Seen[] {
    const last = (s: Seen): number => (s.times.length > 0 ? Math.max(...s.times) : -Infinity);
    return rows.slice().sort((a, b) => (last(b) - last(a)) || a.name.localeCompare(b.name));
}

/** How a name is drawn. The default is text and nothing else. */
export type NameCell = (name: string) => HTMLElement;

function plainName(name: string): HTMLElement {
    const span = document.createElement('span');
    span.textContent = name;
    return span;
}

export function renderSparklines(
    container: HTMLElement,
    label: string,
    rows: Seen[],
    w: Window,
    renderName: NameCell = plainName,
): void {
    const heading = document.createElement('div');
    heading.textContent = label;
    heading.style.color = MUTE;
    heading.style.padding = '0 0 4px';
    heading.style.borderBottom = '1px solid ' + LINE;
    heading.style.marginBottom = '6px';
    container.appendChild(heading);

    if (rows.length === 0) {
        const none = document.createElement('div');
        none.textContent = 'nothing recorded';
        none.style.color = MUTE;
        container.appendChild(none);
        return;
    }

    for (const row of byLastSeen(rows)) {
        const line = document.createElement('div');
        line.className = 'sparkline-row';
        line.style.display = 'flex';
        line.style.alignItems = 'center';
        line.style.gap = '10px';
        line.style.padding = '2px 0';

        const name = renderName(row.name);
        name.style.flex = '1';
        name.style.minWidth = '0';
        name.style.overflowWrap = 'break-word';
        name.style.wordBreak = 'break-word';

        const spark = document.createElement('span');
        spark.className = 'sparkline';
        spark.style.flexShrink = '0';
        spark.style.display = 'inline-flex';
        spark.innerHTML = renderSparkline(seriesOf(row.times, w));

        const last = document.createElement('span');
        last.className = 'sparkline-last';
        last.textContent = row.times.length > 0 ? formatIn(Math.max(...row.times), w.unit) : '';
        last.style.flexShrink = '0';
        last.style.textAlign = 'right';
        last.style.color = MUTE;
        last.style.fontVariantNumeric = 'tabular-nums';

        line.appendChild(name);
        line.appendChild(spark);
        line.appendChild(last);
        container.appendChild(line);
    }
}
