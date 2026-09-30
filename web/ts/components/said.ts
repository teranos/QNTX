/**
 * What QNTX says in tooltip form: an element of @teranos/elements, a new one
 * every time, said beside a line or a ring. "tooltip is a form an element can
 * be in."
 */

import type { TooltipTiming } from '@teranos/elements';

/** The package's own timing: 300ms, then 1s more, and 1.4s to tap again. Tests shorten it. */
export const SAID_TIMING: TooltipTiming = {};

let said = 0;

/** A new element's id, every time. */
export function saidId(prefix: string): string {
    said++;
    return `${prefix}-${said}`;
}
