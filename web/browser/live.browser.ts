// "why not give ci a user"

// "that can run through it"

// "live"

// "on a phone"

// "i dont want to maintain fixtured"

// This pull request's own page, answered by the node itself as its CI user; each element whose content
// cannot be reached is named. A call that is not a GET never leaves the runner.

// The logs of this repository are public: a node is named by tag, class and width, never by its text.

import { test, expect, type Page, type Route } from '@playwright/test';
import { readFileSync } from 'fs';
import WebSocket from 'ws';

/** The node, and its CI user's token (ADR-025). */
const API = process.env.QNTX_GATE_API ?? '';
/** The node's own web origin, which its socket answers. */
const ORIGIN = process.env.QNTX_GATE_ORIGIN ?? '';
const TOKEN = process.env.QNTX_GATE_TOKEN_FILE ? readFileSync(process.env.QNTX_GATE_TOKEN_FILE, 'utf8').trim() : process.env.QNTX_GATE_TOKEN ?? '';

// A trace, a screenshot or a video holds what people wrote, and they would be uploaded where anyone reads them.
test.use({ trace: 'off', screenshot: 'off', video: 'off' });

/** The page's own files, served by serve.ts; everything else is the node's. */
function isStatic(pathname: string): boolean {
    return pathname === '/' || pathname === '/index.html' || pathname === '/qntx.jpg'
        || pathname.startsWith('/js/') || pathname.startsWith('/css/') || pathname.startsWith('/fonts/');
}

/** A call the node has not answered by now is named, not waited on: a stream would never end. */
const ASKED_FOR_MS = 30_000;

interface Wire {
    inflight: number;
    answered: number;
}

/** Asks the node, as the CI user. What would write is refused here and named. */
async function ask(route: Route, wire: Wire, writes: string[], stalled: string[]): Promise<void> {
    const url = new URL(route.request().url());
    const method = route.request().method();
    wire.inflight += 1;
    try {
        if (method !== 'GET' && method !== 'HEAD') {
            writes.push(`${method} ${url.pathname}`);
            await route.fulfill({ status: 405, body: `${method} ${url.pathname}: the live gate only reads` });
            return;
        }
        let answer;
        try {
            answer = await route.fetch({
                url: `${API}${url.pathname}${url.search}`,
                headers: { ...route.request().headers(), authorization: `Bearer ${TOKEN}` },
                timeout: ASKED_FOR_MS,
            });
        } catch (err: unknown) {
            stalled.push(`${method} ${url.pathname}: ${err instanceof Error ? err.message.split('\n')[0] : String(err)}`);
            await route.fulfill({ status: 504, body: `${method} ${url.pathname}: the node did not answer in ${ASKED_FOR_MS}ms` });
            return;
        }
        await route.fulfill({ response: answer });
    } finally {
        wire.inflight -= 1;
        wire.answered += 1;
    }
}

/** How often, and how far apart, a page refused by a restarting node is stood up again. */
const BOOT_TRIES = 3;
const BOOT_WAIT_MS = 30_000;

/** A socket held back (429) or answered by a restarting node's gateway (502, 503, 504): asked again after this long, this many times. */
const SOCKET_WAIT_MS = 15_000;
const SOCKET_TRIES = 4;
const SOCKET_AGAIN = ['429', '502', '503', '504'];

async function databaseStatsAsked(): Promise<string> {
    let last: unknown;
    for (let tried = 1; tried <= SOCKET_TRIES; tried++) {
        try {
            return await databaseStats();
        } catch (err: unknown) {
            last = err;
            if (!(err instanceof Error) || !SOCKET_AGAIN.some((status) => err.message.includes(status))) throw err;
            await new Promise((done) => setTimeout(done, SOCKET_WAIT_MS * tried));
        }
    }
    throw new Error(`the socket refused ${SOCKET_TRIES} times: ${last instanceof Error ? last.message : String(last)}`);
}

/** What the node's socket says of its database when asked, as it says it to the page (server/client.go). */
function databaseStats(): Promise<string> {
    return new Promise((done, fail) => {
        // The node lets a socket in only from a web origin it serves (server/util.go, checkOrigin).
        const socket = new WebSocket(`${API.split('http').join('ws')}/ws`, { headers: { authorization: `Bearer ${TOKEN}`, origin: ORIGIN } });
        const late = setTimeout(() => { socket.terminate(); fail(new Error(`the socket said nothing of its database in ${ASKED_FOR_MS}ms`)); }, ASKED_FOR_MS);
        socket.on('open', () => socket.send(JSON.stringify({ type: 'get_database_stats' })));
        socket.on('error', (err) => { clearTimeout(late); fail(err); });
        socket.on('message', (data) => {
            const said = String(data);
            if (!said.includes('"database_stats"')) return;
            clearTimeout(late);
            socket.close();
            done(said);
        });
    });
}

/** How long the wire is waited on to go still. An element that polls never goes still. */
const QUIET_FOR_MS = 30_000;

/** Whether nothing went over the wire across two frames within the wait: what was drawn is drawn from what was answered. */
async function quiet(page: Page, wire: Wire): Promise<boolean> {
    const until = Date.now() + QUIET_FOR_MS;
    while (Date.now() < until) {
        const before = wire.answered;
        await page.evaluate(() => new Promise<void>((done) => {
            requestAnimationFrame(() => requestAnimationFrame(() => done()));
        }));
        if (wire.inflight === 0 && wire.answered === before) return true;
    }
    return false;
}

/** What cannot be reached in the element, by tag, class and width alone. */
async function unreachable(page: Page, id: string): Promise<string[]> {
    return page.locator(`[data-element-id="${id}"]`).evaluate((element) => {
        const edge = element.getBoundingClientRect();
        const off = edge.left < -1 || edge.right > window.innerWidth + 1
            ? [`the element stands ${Math.round(edge.left)}..${Math.round(edge.right)} on a screen ${window.innerWidth} wide`]
            : [];
        const reaching = Array.from(element.querySelectorAll<HTMLElement>('*')).filter((node) => {
            // An icon's paths are drawn inside its svg, whatever their own geometry: the svg is what is measured.
            if (node.tagName.toLowerCase() !== 'svg' && node.closest('svg') !== null) return false;
            // What is clipped to nothing is kept for a screen reader and hidden from sight on purpose
            // (Elements' forms/panel.ts): it is not content a person is meant to reach by looking.
            for (let at: HTMLElement | null = node; at && at !== element; at = at.parentElement) {
                if (getComputedStyle(at).clip === 'rect(0px, 0px, 0px, 0px)') return false;
            }
            const r = node.getBoundingClientRect();
            if (r.width === 0 && r.height === 0) return false;
            return r.left < edge.left - 1 || r.right > edge.right + 1 || node.scrollWidth > node.clientWidth + 1;
        });
        const innermost = reaching.filter((node) => !reaching.some((other) => other !== node && node.contains(other)));
        const dead = element.querySelector<HTMLElement>('[data-too-wide]');
        // A column past the edge is said once, with how many rows of it are.
        const said = new Map<string, number>();
        for (const node of innermost) {
            const r = node.getBoundingClientRect();
            const classes = typeof node.className === 'string' && node.className !== '' ? `.${node.className.split(' ').filter((c) => c !== '').join('.')}` : '';
            // How it is laid out, so the name says why as well as where: its own box, and what holds it.
            const own = getComputedStyle(node);
            const holder = node.parentElement ? getComputedStyle(node.parentElement) : null;
            const laid = `${own.display} ${own.position} ws=${own.whiteSpace} w=${own.width} minw=${own.minWidth} flex=${own.flex}` +
                (holder ? ` | in ${holder.display}${holder.display.includes('grid') ? ` cols=${holder.gridTemplateColumns}` : ''} ws=${holder.whiteSpace}` : '');
            const line = `${node.tagName.toLowerCase()}${classes} ${Math.round(r.left - edge.left)}..${Math.round(r.right - edge.left)} in ${Math.round(edge.width)}, scrolls ${node.scrollWidth} in ${node.clientWidth} [${laid}]`;
            said.set(line, (said.get(line) ?? 0) + 1);
        }
        return [
            ...off,
            ...Array.from(said, ([line, times]) => times > 1 ? `${line} (×${times})` : line),
            ...(dead ? [`dropped dead: ${dead.dataset.tooWide}`] : []),
        ];
    });
}

test('every element, live, can be reached whole', async ({ page }) => {
    expect(API, 'QNTX_GATE_API names no node: the live gate has nothing to ask').not.toBe('');
    expect(TOKEN, 'QNTX_GATE_TOKEN is empty: the live gate has no user').not.toBe('');
    expect(ORIGIN, 'QNTX_GATE_ORIGIN names no web origin: the node\'s socket answers none').not.toBe('');
    test.setTimeout(15 * 60_000);

    const wire: Wire = { inflight: 0, answered: 0 };
    const writes: string[] = [];
    const stalled: string[] = [];
    const stats = await databaseStatsAsked();
    // The page reads the socket's word at a path (ts/specimens.ts); it is given what the socket said.
    await page.route((url) => url.pathname === '/ws/database_stats', (route) => route.fulfill({ contentType: 'application/json', body: stats }));
    await page.route((url) => !isStatic(url.pathname) && url.pathname !== '/ws/database_stats', (route) => ask(route, wire, writes, stalled));
    // Why a page did not stand, by what refused it and what threw: paths and statuses, never what was answered.
    const said: string[] = [];
    page.on('console', (message) => { if (message.type() === 'error') said.push(`console: ${message.text().split('\n')[0]!.slice(0, 200)}`); });
    page.on('pageerror', (err) => { said.push(`threw: ${err.message.split('\n')[0]!.slice(0, 200)}`); });
    page.on('response', (response) => {
        if (response.status() >= 400) said.push(`${response.request().method()} ${new URL(response.url()).pathname} ${response.status()}`);
    });
    // A node restarting answers its gateway's 502, 503 or 504 for a while, and a slow node turns its
    // heaviest token away with a 429 until it lifts (server/auth/shed.go): the page is stood up again
    // after a wait. Anything else that refuses it, or a node still refusing after the last try, is said.
    let stood = false;
    for (let tried = 1; tried <= BOOT_TRIES && !stood; tried++) {
        said.length = 0;
        await page.goto('/');
        stood = await page.waitForFunction(() => window.specimens !== undefined, null, { timeout: 60_000 }).then(() => true, () => false);
        const restarting = said.some((line) => line.endsWith(' 429') || line.endsWith(' 502') || line.endsWith(' 503') || line.endsWith(' 504'));
        if (!stood && !restarting) break;
        if (!stood && tried < BOOT_TRIES) await new Promise((done) => setTimeout(done, BOOT_WAIT_MS));
    }
    if (!stood) {
        await page.goto('about:blank');
        throw new Error(`the page did not stand: ${said.join(' | ') || 'nothing was refused and nothing threw'}`);
    }
    await quiet(page, wire);
    const ids = await page.evaluate(() => window.specimens!.ids());

    const bad: Record<string, string[]> = {};
    for (const id of ids) {
        console.log(`opening ${id}`);
        try {
            await page.evaluate((id) => window.specimens!.open(id), id);
            const element = page.locator(`[data-element-id="${id}"]`);
            await expect.poll(() => element.evaluate((el) => (el as HTMLElement).dataset.form), { message: `${id} is still a dot` }).not.toBe('dot');
            await expect.poll(() => element.evaluate((el) => el.classList.contains('morphing'))).toBe(false);
            // An element that asks the node all the time is never quiet: it is measured once the wait is over.
            await quiet(page, wire);
            const past = await unreachable(page, id);
            if (past.length > 0) bad[id] = past;
        } catch (err: unknown) {
            // One that never stands is named with the rest, and the next one still opens.
            bad[id] = [`did not stand: ${err instanceof Error ? err.message.split('\n')[0] : String(err)}`];
        }
    }
    // A failing expect writes the page it saw beside the report: the page is blank before one can.
    await page.goto('about:blank');
    expect.soft(stalled, 'the node did not answer these').toEqual([]);
    expect.soft(writes, 'an element opening tried to write to the node').toEqual([]);
    expect(bad, `${Object.keys(bad).length} of ${ids.length} elements have content that cannot be reached`).toEqual({});
});
