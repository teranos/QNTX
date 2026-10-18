/**
 * The elements with no node behind them, for the browser gate (web/browser).
 */

// "is the GitHub perfectly respecting of mobile users, YES/NO — if NO ci needs to be RED"
// "if it overflows width, hard fail, die"

// The real tray and the real elements. Their API is answered by the node itself,
// asked by the live gate as its CI user (browser/live.browser.ts).

// A test DOM lays nothing out, so an element too wide for a phone is measured
// here, in Chromium and WebKit, and nowhere else.

import { configureElements, expectFits, tray } from '@teranos/elements';
import { registerDefaultElements, updateDatabaseStats, updateSigmaPanel } from './default-elements.ts';
import { apiJson } from './client/http';
import { openUserElement } from './user-element.ts';
import { MELDABILITY } from './components/element/meld/meldability';
import { setStanding } from './standing.ts';
import { person } from './self-person.ts';
import { log, SEG } from './logger.ts';
import type { UserRecord } from './users-element.ts';

/** What the gate drives, on window. */
export interface Specimens {
    /** Every element on the page, by id. */
    ids(): string[];
    /** Opens one: a window is what it becomes. */
    open(id: string): void;
    /** Throws, in the package's words, when anything in the element is wider than it. */
    fits(id: string): void;
    /** The User window, opened from Users, on a User the node lists. */
    openUser(u: UserRecord): void;
}

declare global {
    interface Window {
        specimens?: Specimens;
    }
}

function ids(): string[] {
    const seen = new Set<string>();
    for (const el of document.querySelectorAll<HTMLElement>('[data-element-id]')) {
        if (el.dataset.elementId) seen.add(el.dataset.elementId);
    }
    return Array.from(seen);
}

function fits(id: string): void {
    const element = document.querySelector<HTMLElement>(`[data-element-id="${id}"]`);
    if (!element) {
        throw new Error(`no element ${id} on the page`);
    }
    expectFits(element);
}

async function stand(): Promise<void> {
    configureElements({ logger: log, logSegment: SEG.ELEMENT, meldability: MELDABILITY });
    // Where the node says this person stands, as main.ts asks it: the live gate's user stands where it was minted.
    setStanding((await person()).standing);
    tray.init();
    await registerDefaultElements(true);
    // What the socket pushes: the gate asks the node's socket and gives it here (browser/live.browser.ts).
    const stats = await apiJson<Record<string, unknown>>('/ws/database_stats');
    updateDatabaseStats(stats);
    updateSigmaPanel(stats);
    document.getElementById('loading-screen')?.remove();
    document.body.classList.remove('disconnected');
    window.specimens = { ids, open: (id) => tray.open(id), fits, openUser: openUserElement };
}

stand().catch((err: unknown) => {
    log.error(SEG.UI, '[Specimens] the page did not stand', err);
});
