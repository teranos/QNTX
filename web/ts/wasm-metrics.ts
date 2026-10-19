/**
 * How long each call into the WASM we ship takes, as a Sentry distribution
 * (docs/sentry.md). The same off switch as everything else that leaves: no
 * DSN, no client, and the metric is discarded.
 */

import * as Sentry from '@sentry/browser';
import { timer } from './wasm-timer';

export const { timed, timedAsync } = timer(
    (module, fn, ms, attributes) => {
        Sentry.metrics.distribution('wasm.call.duration', ms, {
            unit: 'millisecond',
            attributes: { 'wasm.module': module, 'wasm.function': fn, ...attributes },
        });
    },
    () => performance.now(),
);
