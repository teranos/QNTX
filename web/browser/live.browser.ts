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

/** The node holds back a socket asked for too often (429): asked again after this long, this many times. */
const SOCKET_WAIT_MS = 10_000;
const SOCKET_TRIES = 3;

async function databaseStatsAsked(): Promise<string> {
    let last: unknown;
    for (let tried = 1; tried <= SOCKET_TRIES; tried++) {
        try {
            return await databaseStats();
        } catch (err: unknown) {
            last = err;
            if (!(err instanceof Error) || !err.message.includes('429')) throw err;
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

async function quiet(page: Page, wire: Wire): Promise<void> {
    await expect.poll(async () => {
        const before = wire.answered;
        await page.evaluate(() => new Promise<void>((done) => {
            requestAnimationFrame(() => requestAnimationFrame(() => done()));
        }));
        return wire.inflight === 0 && wire.answered === before;
    }, { message: 'the element is still asking the node', timeout: 30_000 }).toBe(true);
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
            const line = `${node.tagName.toLowerCase()}${classes} ${Math.round(r.left - edge.left)}..${Math.round(r.right - edge.left)} in ${Math.round(edge.width)}, scrolls ${node.scrollWidth} in ${node.clientWidth}`;
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
    await page.goto('/');
    await expect.poll(() => page.evaluate(() => window.specimens !== undefined), { message: `the page did not stand: ${said.join(' | ')}`, timeout: 60_000 }).toBe(true);
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
