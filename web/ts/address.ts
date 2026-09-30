/**
 * Where this tab is, in its address: `?ns=<namespace>&canvas=<id>`.
 *
 * Where a person stands is kept on the person, not on the tab (ADR-026), so a
 * second tab stepping elsewhere moved the first tab's writes with it while the
 * first still drew the namespace it was built for. The address is the tab's
 * own: a tab that comes back asks to stand where its address says before
 * anything is written.
 *
 * The address asks, it does not decide. Asking is the same POST /i/standing a
 * press on the namespaces bar sends, so a namespace that is off, not served,
 * or not reachable from where this person came in is refused just the same.
 */

import { apiFetch } from './client';
import { jsonBody } from './http-utils';
import { log, SEG } from './logger.ts';
import { standingNamespace } from './standing.ts';

export interface Address {
    ns: string;
    /** Empty asks for the canvas remembered in that namespace. */
    canvas: string;
}

/** What the address asks for, or null when it names no namespace. */
export function addressed(search: string = location.search): Address | null {
    const params = new URLSearchParams(search);
    const ns = params.get('ns') ?? '';
    if (ns === '') return null;
    return { ns, canvas: params.get('canvas') ?? '' };
}

/** The address of a namespace and canvas, keeping whatever else it carries. */
export function addressOf(ns: string, canvas: string, href: string = location.href): string {
    const url = new URL(href);
    url.searchParams.set('ns', ns);
    if (canvas === '') url.searchParams.delete('canvas');
    else url.searchParams.set('canvas', canvas);
    return url.toString();
}

/** Writes where the page was built for into its address, adding no history. */
export function settle(ns: string, canvas: string): void {
    if (ns === '') return;
    history.replaceState(history.state, '', addressOf(ns, canvas));
}

/**
 * Names the tab after where it is, so tabs in different namespaces and
 * canvases are told apart in the browser's tab bar. The namespace's own
 * canvas is the namespace; another canvas is named beside it.
 */
export function entitle(ns: string, canvas = ''): void {
    if (ns === '') {
        document.title = 'QNTX';
        return;
    }
    document.title = canvas === '' ? `${ns} — QNTX` : `${ns} · ${canvas} — QNTX`;
}

/** Builds the page for another namespace or canvas: a new entry, so back returns. */
export function go(ns: string, canvas: string): void {
    location.assign(addressOf(ns, canvas));
}

/** Asks to stand in a namespace. The answer is where this person now stands. */
export async function stepTo(ns: string): Promise<string> {
    const response = await apiFetch('/i/standing', jsonBody('POST', { namespace: ns }));
    if (!response.ok) {
        throw new Error(`could not stand in ${ns}: HTTP ${response.status} ${await response.text()}`);
    }
    return (await response.json() as { namespace: string }).namespace;
}

async function whereStanding(): Promise<string> {
    const response = await apiFetch('/i/standing');
    if (!response.ok) {
        throw new Error(`could not read where you stand: HTTP ${response.status} ${await response.text()}`);
    }
    return (await response.json() as { namespace: string }).namespace;
}

// A refusal is said in the namespaces bar, where stepping is pressed. The page
// may be built again before the bar is up, so it is kept for this tab until
// the bar says it.
const TOLD = 'qntx-told';
let hear: (() => void) | null = null;

/**
 * Says a refusal in the namespaces bar, now or once it is up. A page about to
 * be built again says it in the next one.
 */
export function tell(said: string, leaving = false): void {
    try {
        window.sessionStorage.setItem(TOLD, said);
    } catch (err: unknown) {
        log.warn(SEG.UI, `[Address] Could not keep "${said}" for the namespaces bar:`, err);
    }
    if (!leaving) hear?.();
}

/** The refusal waiting to be said, once. */
export function told(): string {
    try {
        const said = window.sessionStorage.getItem(TOLD) ?? '';
        window.sessionStorage.removeItem(TOLD);
        return said;
    } catch (err: unknown) {
        log.warn(SEG.UI, '[Address] Could not read what the namespaces bar was to say:', err);
        return '';
    }
}

/** The namespaces bar, listening for a refusal while it is up. */
export function onTold(listener: (() => void) | null): void {
    hear = listener;
}

let returning: Promise<void> | null = null;

/**
 * Stands back where this tab was built for, when another tab stepped
 * elsewhere. A step refused is a tab that can no longer act where it draws, so
 * it is built again for where this person does stand.
 */
export function returnHere(): Promise<void> {
    const here = standingNamespace();
    if (here === '') return Promise.resolve();
    if (returning) return returning;
    returning = (async () => {
        try {
            const there = await whereStanding();
            if (there === here) return;
            let now = there;
            try {
                now = await stepTo(here);
            } catch (err: unknown) {
                log.warn(SEG.UI, `[Address] This tab is ${here}, another stepped to ${there}:`, err);
            }
            if (now === here) {
                log.info(SEG.UI, `[Address] Another tab stepped to ${there}; this tab stands in ${here} again`);
                return;
            }
            tell(`Another tab stepped to ${now} and this tab could not stand in ${here} again, so it opens ${now}`, true);
            go(now, '');
        } catch (err: unknown) {
            log.warn(SEG.UI, `[Address] Could not tell whether this tab still stands in ${here}:`, err);
        } finally {
            returning = null;
        }
    })();
    return returning;
}

/** A tab that is looked at again is where its address says. */
export function keepTabWhereItIs(): void {
    window.addEventListener('focus', () => { void returnHere(); });
    document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'visible') void returnHere();
    });
}
