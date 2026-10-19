/**
 * What the door's field costs, as Sentry counters (docs/sentry.md). The same
 * off switch as everything else that leaves: no DSN, no client, and the metric
 * is discarded.
 */

import * as Sentry from '@sentry/browser';
import { meter, type Meter, type Attributes } from './door-meter';

export function meterField(where: () => Attributes): Meter {
    return meter(
        (name, value, unit, attributes) => {
            Sentry.metrics.count(name, value, { ...(unit ? { unit } : {}), attributes });
        },
        () => performance.now(),
        where,
    );
}
