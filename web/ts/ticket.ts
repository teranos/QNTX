/**
 * One arrival path. A ceremony that ran somewhere else ends by sending the
 * person back to the door with a ticket. The app is handed it through the deep
 * link; a browser finds it in the URL it landed on. From the ticket on, the two
 * are the same door: collect the binding it names, land it, go on in.
 *
 * "i agree with one arrival path"
 */

import { inApp, ticketWaiting } from './app-door';

/** The ticket in a query string, or null when it carries none. */
export function ticketInQuery(query: string): string | null {
    const cut = query.indexOf('?');
    const raw = cut === -1 ? query : query.slice(cut + 1);
    return new URLSearchParams(raw).get('ceremony') || null;
}

/**
 * The same URL without the ticket. A ticket is spent on collection, so the
 * address the browser keeps must not carry it: a reload would ask the node
 * for a binding it has already forgotten.
 */
export function withoutTicket(href: string): string {
    const cut = href.indexOf('?');
    if (cut === -1) return href;
    const hash = href.indexOf('#', cut);
    const query = new URLSearchParams(href.slice(cut + 1, hash === -1 ? undefined : hash));
    query.delete('ceremony');
    const rest = query.toString();
    return href.slice(0, cut) + (rest ? '?' + rest : '') + (hash === -1 ? '' : href.slice(hash));
}

/**
 * The ticket this page arrived with, or null when it arrived with none. In a
 * browser the ticket leaves the address bar on the way out.
 */
export async function arrivedWith(): Promise<string | null> {
    if (inApp()) return ticketWaiting();
    const here = window.location.href;
    const ticket = ticketInQuery(here);
    if (ticket) window.history.replaceState(window.history.state, '', withoutTicket(here));
    return ticket;
}
