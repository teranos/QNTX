/**
 * Backend URL resolution — single source of truth.
 *
 * Leaf module with no imports from client/. Safe to import from any submodule
 * without risk of circular dependencies.
 */

import { stripProtocol } from '../http-utils';

/** Backend base URL from injected global or current origin */
export function backendUrl(): string {
    return (window as any).__BACKEND_URL__ || window.location.origin;
}

/** WebSocket URL (ws[s]://host) — no regex, uses stripProtocol */
export function backendWsUrl(): string {
    const url = backendUrl();
    const host = stripProtocol(url);
    const protocol = url.startsWith('https') ? 'wss:' : 'ws:';
    return `${protocol}//${host}`;
}

/**
 * Full URL for EventSource, img src, fetch — anything the page *loads*.
 *
 * Not for a script. A browser judges executable code under `script-src` and
 * everything here under `connect-src`, `img-src` and their like, and a
 * deployment may permit the node for one and not the other. importScript below
 * is the one that knows the difference — import it from this module directly,
 * not through the client facade, which cannot hand it out without a cycle.
 */
export function backendPath(path: string): string {
    return backendUrl() + path;
}

/**
 * Where this page may import a module from, remembered once something answers.
 *
 * Two pages read the same node and cannot use one URL for its code:
 *
 * A page behind an edge that forwards the path imports it same-origin, because
 * that is what `script-src 'self'` permits. Asking the node directly is refused
 * before a request is made, with nothing in the network log to find.
 *
 * An app at its own scheme has no edge in front of it. Asked of itself the path
 * answers with the app's own index.html — "'text/html' is not a valid
 * JavaScript MIME type" — so it has to ask the node.
 *
 * Nothing the page can read says which it is. backendPath answers where the
 * node is, which is the other question: right for a fetch, wrong for this, and
 * named so closely that reaching for it here is the easy mistake. It is the one
 * that put crier, hello and doc back behind a CSP refusal after an app fix.
 */
let importsFrom: 'page' | 'node' | null = null;

/**
 * How a URL is turned into a module. The real one is the browser's.
 *
 * Named so this rule can be exercised against something other than a live
 * page: a test cannot arrange a CSP refusal or an app scheme, and testing a
 * copy of the rule instead of the rule proves nothing about the rule.
 */
export type Loads = (url: string) => Promise<Record<string, unknown>>;

const byImport: Loads = url => import(/* @vite-ignore */ url);

/**
 * Import a module from the node, whichever way this page is allowed to.
 *
 * One import decides it and every one after goes straight there. A failure
 * names both attempts, because each is silent in its own way — one never
 * reaches the network, the other comes back as HTML with a 200 — and remembers
 * nothing, so the next module asks again rather than inheriting a guess.
 */
export async function importScript(path: string, loads: Loads = byImport): Promise<Record<string, unknown>> {
    const fromPage = path;
    const fromNode = backendPath(path);

    // The node is this page's own origin — a node serving its own UI, or the
    // dev server proxying to it. There are not two places to ask, so there is
    // nothing to choose and nothing to remember.
    if (backendUrl() === window.location.origin) return loads(fromPage);

    if (importsFrom === 'page') return loads(fromPage);
    if (importsFrom === 'node') return loads(fromNode);

    try {
        const held = await loads(fromPage);
        importsFrom = 'page';
        return held;
    } catch (fromPageErr) {
        try {
            const held = await loads(fromNode);
            importsFrom = 'node';
            return held;
        } catch (fromNodeErr) {
            throw new Error(
                `${fromPage} did not load (${String(fromPageErr)}) ` +
                `and neither did ${fromNode} (${String(fromNodeErr)})`,
            );
        }
    }
}

/** Which of the two answered, or nothing until one has. For a log line. */
export function scriptsComeFrom(): 'page' | 'node' | null {
    return importsFrom;
}

/** Forget which answered. For a test, and for nothing on a page. */
export function forgetWhereScriptsComeFrom(): void {
    importsFrom = null;
}
