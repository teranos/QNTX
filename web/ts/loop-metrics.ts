/**
 * What the page's loops cost, as Sentry counters (docs/sentry.md). The same off
 * switch as everything else that leaves: no DSN, no client, and the metric is
 * discarded.
 *
 * A loop says `meterLoop('its.name')` where it starts, and `.report('stop')`
 * where it stops. When the page is hidden or left, every loop still standing
 * reports without being asked.
 */

import * as Sentry from '@sentry/browser';
import { loops, type Attributes, type Meter } from './loop-meter';

const standing = loops(
    (name, value, unit, attributes) => {
        Sentry.metrics.count(name, value, { ...(unit ? { unit } : {}), attributes });
    },
    () => performance.now(),
);

let listening = false;

function listen(): void {
    if (listening) return;
    listening = true;
    document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'hidden') standing.reportAlive('hidden');
    });
    window.addEventListener('pagehide', () => standing.reportAlive('pagehide'));
}

export function meterLoop(name: string, where?: () => Attributes): Meter {
    listen();
    return standing.start(name, where);
}
