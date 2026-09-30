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

import { tooltip } from './tooltip';

const MUTE = 'var(--text-on-dark-tertiary)';
const LINE = 'var(--border-on-dark)';

/** A value for an HTML attribute, whatever it holds. */
function attr(value: string): string {
    return value.split('&').join('&amp;').split('"').join('&quot;').split('<').join('&lt;');
}

/**
 * Draw one series as an 80×16 line, scaled to its own maximum. Given when each
 * step is, the line answers pointing: when, and the value then, and a longer
 * hover draws the whole of it (the tooltip, components/tooltip.ts).
 */
export function renderSparkline(data: (number | null)[], at?: string[]): string {
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

    const steps = at && at.length === data.length
        ? ` data-tooltip-series="${attr(JSON.stringify(data.map((v, i) => [at[i], v ?? 0])))}"`
        : '';
    return `<svg class="sparkline-line"${steps} viewBox="0 0 ${w} ${h}" style="width: ${w}px; height: ${h}px;">
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

/** How many moments fall in each bucket of the window, empty buckets at zero.
 *  A moment the node already counted comes with how many it stands for. */
export function seriesOf(times: number[], w: Window, weights?: number[]): number[] {
    const starts: number[] = [];
    for (let t = bucketStart(w.start, w.unit); t <= w.end; t = nextBucket(t, w.unit)) {
        starts.push(t);
    }
    const counts = starts.map(() => 0);
    times.forEach((t, k) => {
        const i = starts.indexOf(bucketStart(t, w.unit));
        if (i >= 0) counts[i] += weights ? weights[k] : 1;
    });
    return counts;
}

const pad = (n: number): string => String(n).padStart(2, '0');

/** When each step of a window's series is, in its unit's format: seriesOf's buckets. */
export function labelsOf(w: Window): string[] {
    const labels: string[] = [];
    for (let t = bucketStart(w.start, w.unit); t <= w.end; t = nextBucket(t, w.unit)) {
        labels.push(formatIn(t, w.unit));
    }
    return labels;
}

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

/** A name and every moment it was seen. When the node sends buckets rather
 *  than moments, each time is a bucket's start and its weight how many fell in
 *  it, and last is the moment itself. */
export interface Seen {
    name: string;
    times: number[];
    weights?: number[];
    last?: number;
}

/**
 * The start of a bucket the node names by its key, in UTC: `2026-09-11T14:10`
 * (ten minutes), `2026-09-11T14` (an hour), `2026-09-11` (a day) or `2026-W37`
 * (an ISO week). Null for a key in none of these shapes.
 */
export function timeOfBucket(key: string): number | null {
    let t = NaN;
    if (key.length === 8 && key.slice(4, 6) === '-W') {
        const year = Number(key.slice(0, 4));
        const week = Number(key.slice(6));
        const jan4 = Date.UTC(year, 0, 4);
        const monday = jan4 - ((new Date(jan4).getUTCDay() + 6) % 7) * 86_400_000;
        t = monday + (week - 1) * 7 * 86_400_000;
    } else if (key.length === 16) {
        t = Date.parse(`${key}:00Z`);
    } else if (key.length === 13) {
        t = Date.parse(`${key}:00:00Z`);
    } else if (key.length === 10) {
        t = Date.parse(`${key}T00:00:00Z`);
    }
    return Number.isNaN(t) ? null : t;
}

/** A name seen by the bucket, as the node sends it: `{ "2026-09-11T14": 3 }`. */
export function seenOver(name: string, over: Record<string, number> | null | undefined, last?: string): Seen {
    const times: number[] = [];
    const weights: number[] = [];
    for (const [key, n] of Object.entries(over ?? {})) {
        const t = timeOfBucket(key);
        if (t === null || n <= 0) continue;
        times.push(t);
        weights.push(n);
    }
    const at = last ? Date.parse(last) : NaN;
    return { name, times, weights, last: Number.isNaN(at) ? undefined : at };
}

/** When a name was last seen, or -Infinity for never. */
export function lastOf(s: Seen): number {
    if (s.last !== undefined) return s.last;
    return s.times.length > 0 ? Math.max(...s.times) : -Infinity;
}

/** Most recently seen first; a tie reads the same way twice, by name. */
export function byLastSeen(rows: Seen[]): Seen[] {
    const last = lastOf;
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
        spark.innerHTML = renderSparkline(seriesOf(row.times, w, row.weights), labelsOf(w));

        const last = document.createElement('span');
        last.className = 'sparkline-last';
        const at = lastOf(row);
        last.textContent = at === -Infinity ? '' : formatIn(at, w.unit);
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

/**
 * The bigger picture of one line, for a longer hover: the whole line drawn
 * large between the first and last moment of its window, and every moment
 * that saw something, with its value.
 */
export function wholeLine(from: HTMLElement): HTMLElement | null {
    let steps: [string, number][];
    try {
        steps = JSON.parse(from.dataset.tooltipSeries ?? '[]') as [string, number][];
    } catch (err: unknown) {
        const said = document.createElement('div');
        said.textContent = `this line's moments could not be read: ${err instanceof Error ? err.message : String(err)}`;
        return said;
    }
    if (steps.length < 2) return null;

    const picture = document.createElement('div');
    picture.className = 'sparkline-whole';

    const w = 320;
    const h = 64;
    const max = Math.max(...steps.map(([, v]) => v));
    const points = steps.map(([, v], i) => {
        const x = (i / (steps.length - 1)) * w;
        const y = max === 0 ? h - 1 : h - (v / max) * (h - 2) - 1;
        return `${x},${y}`;
    });
    picture.innerHTML = `<svg viewBox="0 0 ${w} ${h}" style="width: ${w}px; height: ${h}px; display: block;">
        <polyline points="${points.join(' ')}" fill="none" stroke="#94a3b8" stroke-width="1.5" />
    </svg>`;

    const axis = document.createElement('div');
    axis.className = 'sparkline-whole-axis';
    axis.style.display = 'flex';
    axis.style.justifyContent = 'space-between';
    axis.style.color = MUTE;
    const first = document.createElement('span');
    first.textContent = steps[0][0];
    const last = document.createElement('span');
    last.textContent = steps[steps.length - 1][0];
    axis.append(first, last);
    picture.appendChild(axis);

    const moments = document.createElement('div');
    moments.className = 'sparkline-whole-moments';
    moments.style.marginTop = '6px';
    for (const [at, value] of steps) {
        if (value === 0) continue;
        const moment = document.createElement('div');
        moment.textContent = `${at} · ${value}`;
        moments.appendChild(moment);
    }
    picture.appendChild(moments);
    return picture;
}

tooltip.expands('data-tooltip-series', wholeLine);
