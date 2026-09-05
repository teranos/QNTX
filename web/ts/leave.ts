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
 * One logger line, leaving. Below error it is a breadcrumb: context for the
 * event that follows, nothing on its own. At error it becomes an issue, and
 * the error itself when one was on the line, so the issue carries its stack.
 */
export function left(level: Level, context: string, message: string, args: unknown[]): void {
    if (level === 'debug') return;
    if (level !== 'error') {
        // Sentry spells the level 'warning'; the logger spells it 'warn'.
        const severity = level === 'warn' ? 'warning' : 'info';
        Sentry.addBreadcrumb({ category: context, message, level: severity, data: dataOf(args) });
        return;
    }
    const err = errorIn(args);
    const scope = { tags: { context }, extra: dataOf(args) };
    if (err) {
        Sentry.captureException(err, { ...scope, extra: { ...scope.extra, message } });
    } else {
        Sentry.captureMessage(`[${context}] ${message}`, { ...scope, level: 'error' });
    }
}

/** The rest of the line, as it was written, keyed by position. */
function dataOf(args: unknown[]): Record<string, unknown> | undefined {
    if (args.length === 0) return undefined;
    const data: Record<string, unknown> = {};
    args.forEach((a, i) => { data[String(i)] = a instanceof Error ? String(a) : a; });
    return data;
}
