/**
 * The console is for whoever has the tab open. This is for when nobody does.
 *
 * A third sink teed onto the logger, the way the node's is teed onto its
 * global logger (docs/sentry.md). No call site says Sentry, no module imports
 * this but the logger, and turning it off is one empty string: the DSN the
 * deploy handed the build, or nothing.
 *
 * "we are missing a lot of observability here"
 */

import * as Sentry from '@sentry/browser';

/** What the build stamped into the page. build.ts writes both. */
interface Stamped {
    __SENTRY_DSN__?: string;
    __QNTX_WEB_BUILD__?: { commit: string; build_time: string; qntx: string };
}

export interface Leaving {
    dsn: string;
    release: string;
}

/**
 * Whether anything leaves, and under which release. Null is off: no DSN was
 * handed to the build, so every call below is a method on a client that does
 * not exist and discards it.
 */
export function leavingFrom(stamped: Stamped): Leaving | null {
    const dsn = stamped.__SENTRY_DSN__?.trim() ?? '';
    if (!dsn) return null;
    return { dsn, release: stamped.__QNTX_WEB_BUILD__?.qntx ?? 'unknown' };
}

/** Starts the client, once, before anything else runs. Off is a no-op. */
export function leave(stamped: Stamped): Leaving | null {
    const going = leavingFrom(stamped);
    if (!going) return null;
    Sentry.init({
        dsn: going.dsn,
        release: going.release,
        // The page says which door it is; a hostname is the deployment.
        environment: window.location.hostname,
        sendDefaultPii: false,
    });
    return going;
}

export type Level = 'debug' | 'info' | 'warn' | 'error';

/** The error among what was logged, if one was. It goes out as an error. */
function errorIn(args: unknown[]): Error | null {
    for (const a of args) if (a instanceof Error) return a;
    return null;
}

/**
 * One logger line, leaving. Every line at info and above is a log item in
 * the stream, as it was written, with its context as an attribute. At error
 * it becomes two things, as on the node: the log line, and an issue. The issue
 * carries the error itself when one was on the line, so it has its stack.
 */
export function left(level: Level, context: string, message: string, args: unknown[]): void {
    if (level === 'debug') return;
    const attrs = { context, ...attributesOf(args) };
    Sentry.logger[level](message, attrs);
    if (level !== 'error') return;
    const err = errorIn(args);
    if (err) {
        Sentry.captureException(err, { tags: { context }, extra: { message, ...attrs } });
    } else {
        Sentry.captureMessage(`[${context}] ${message}`, { tags: { context }, extra: attrs, level: 'error' });
    }
}

/**
 * The rest of the line, keyed by position. A log attribute is a string, a
 * number or a boolean; anything else goes as what JSON makes of it, and an
 * Error as its message, since its stack rides the issue.
 */
function attributesOf(args: unknown[]): Record<string, string | number | boolean> {
    const attrs: Record<string, string | number | boolean> = {};
    args.forEach((a, i) => {
        const key = String(i);
        if (typeof a === 'string' || typeof a === 'number' || typeof a === 'boolean') attrs[key] = a;
        else if (a instanceof Error) attrs[key] = String(a);
        else attrs[key] = JSON.stringify(a) ?? String(a);
    });
    return attrs;
}
