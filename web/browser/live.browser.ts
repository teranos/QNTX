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
import { dirname, join } from 'path';
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
    /** What the node refused, as method, path and status, since the element now opening began. */
    refused: string[];
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
        // A 404 is the node saying nothing is there, which is absence, not a refusal (CLAUDE.md, nil is nil).
        if (answer.status() >= 400 && answer.status() !== 404) wire.refused.push(`${method} ${url.pathname} ${answer.status()}`);
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
            // Cut off from below too: a box that hides what is taller than it, and is not a scroller.
            const y = getComputedStyle(node).overflowY;
            const cut = (y === 'hidden' || y === 'clip') && node.scrollHeight > node.clientHeight + 1;
            return r.left < edge.left - 1 || r.right > edge.right + 1 || node.scrollWidth > node.clientWidth + 1 || cut;
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
            const tall = node.scrollHeight > node.clientHeight + 1 && (own.overflowY === 'hidden' || own.overflowY === 'clip') ? `, cut ${node.clientHeight} of ${node.scrollHeight} high` : '';
            const line = `${node.tagName.toLowerCase()}${classes} ${Math.round(r.left - edge.left)}..${Math.round(r.right - edge.left)} in ${Math.round(edge.width)}, scrolls ${node.scrollWidth} in ${node.clientWidth}${tall} [${laid}]`;
            said.set(line, (said.get(line) ?? 0) + 1);
        }
        return [
            ...off,
            ...Array.from(said, ([line, times]) => times > 1 ? `${line} (×${times})` : line),
            ...(dead ? [`dropped dead: ${dead.dataset.tooWide}`] : []),
        ];
    });
}

// "claude element should not start at top when opening requiring user to scroll down every single time"

// "the inputfield take up the entire bottom of the screen for chat elements like claude or pi"

/** Where what was said last, or the field it is said from, is not where Elements puts it, on what the node holds. */
async function misheld(page: Page, id: string): Promise<string[]> {
    return page.locator(`[data-element-id="${id}"]`).evaluate((element) => {
        const said: string[] = [];
        const el = element as HTMLElement;
        if (el.dataset.opensAt === 'end') {
            const scroller = el.querySelector<HTMLElement>('[data-scroller="declared"]') ?? el.querySelector<HTMLElement>(':scope > [data-scroller="body"]');
            if (scroller === null) said.push('opens at its end, and has no scroller to hold it there');
            else {
                const short = Math.round(scroller.scrollHeight - scroller.clientHeight - scroller.scrollTop);
                if (short > 2) said.push(`opens ${short}px short of its end, in ${scroller.clientHeight} of ${scroller.scrollHeight}`);
            }
            const second = el.querySelectorAll('[data-scroller="undeclared"]').length;
            if (second > 0) said.push(`opens at its end, and ${second} more ${second === 1 ? 'thing scrolls' : 'things scroll'} in it`);
        }
        const field = el.querySelector<HTMLElement>('[data-say]');
        const row = field?.parentElement;
        if (field && row) {
            const f = field.getBoundingClientRect();
            const r = row.getBoundingClientRect();
            if (f.width <= r.width - 16) said.push(`its field is ${Math.round(f.width)} wide in a row of ${Math.round(r.width)}`);
            if (f.bottom <= r.bottom - 8) said.push(`its field ends ${Math.round(r.bottom - f.bottom)}px above the bottom of its row`);
        }
        return said;
    });
}

// "this doc is important for us"

/** Each target in the element smaller than WCAG 2.2's 2.5.8 allows, where its 24 CSS px circle meets another target. */
async function undersized(page: Page, id: string): Promise<string[]> {
    return page.locator(`[data-element-id="${id}"]`).evaluate((element) => {
        const MIN = 24;
        const name = (node: HTMLElement) => `${node.tagName.toLowerCase()}${typeof node.className === 'string' && node.className !== '' ? `.${node.className.split(' ').filter((c) => c !== '').join('.')}` : ''}`;
        const targets = Array.from(element.querySelectorAll<HTMLElement>('button, a[href], input, select, textarea, [role="button"]')).filter((node) => {
            const r = node.getBoundingClientRect();
            // A box with no width or no height draws nothing to point at.
            if (r.width === 0 || r.height === 0) return false;
            // "Inline": a target in a sentence is sized by the line it stands in.
            return getComputedStyle(node).display !== 'inline';
        });
        const small = (r: DOMRect) => r.width < MIN || r.height < MIN;
        const centre = (r: DOMRect) => ({ x: r.left + r.width / 2, y: r.top + r.height / 2 });
        // A row of targets alike is said once, with how many of them are.
        const said = new Map<string, number>();
        for (const target of targets) {
            const r = target.getBoundingClientRect();
            if (!small(r)) continue;
            const c = centre(r);
            // "Spacing": its circle does not meet another target, nor the circle of another undersized one.
            const meets = targets.find((other) => {
                if (other === target || target.contains(other) || other.contains(target)) return false;
                const o = other.getBoundingClientRect();
                const dx = Math.max(o.left - c.x, 0, c.x - o.right);
                const dy = Math.max(o.top - c.y, 0, c.y - o.bottom);
                if (Math.hypot(dx, dy) < MIN / 2) return true;
                const oc = centre(o);
                return small(o) && Math.hypot(oc.x - c.x, oc.y - c.y) < MIN;
            });
            if (!meets) continue;
            const line = `target ${name(target)} ${Math.round(r.height) < MIN ? `${Math.round(r.height)} high` : `${Math.round(r.width)} wide`}, under ${MIN}×${MIN}, its circle meets ${name(meets)}`;
            said.set(line, (said.get(line) ?? 0) + 1);
        }
        return Array.from(said, ([line, times]) => times > 1 ? `${line} (×${times})` : line);
    });
}

// "we are probably miscompliant on this as well"

/** Each control in the element that is disabled: it takes no focus, and says nothing of why. */
async function disabled(page: Page, id: string): Promise<string[]> {
    return page.locator(`[data-element-id="${id}"]`).evaluate((element) =>
        Array.from(element.querySelectorAll<HTMLButtonElement>('button, input, select, textarea'))
            .filter((node) => node.disabled)
            .map((node) => `${node.tagName.toLowerCase()}${typeof node.className === 'string' && node.className !== '' ? `.${node.className.split(' ').filter((c) => c !== '').join('.')}` : ''} is disabled`));
}

// "turn the miscompliance into a burn down"

// "we wont solve all of it now"

// "so, just like nil is nil"

/** The debt: what an element does not yet meet, per project, element and what it owes. Each number may fall and may not rise. */
function burnDown(): { path: string; lines: Map<string, number> } {
    const config = test.info().config.configFile;
    if (config === undefined) throw new Error('playwright names no config file, so the burn-down beside it cannot be found');
    const path = join(dirname(config), 'owed');
    const lines = new Map<string, number>();
    for (const line of readFileSync(path, 'utf8').split('\n')) {
        if (line === '' || line.startsWith('#')) continue;
        const tab = line.indexOf('\t');
        const count = Number(line.slice(0, tab));
        if (tab < 0 || !Number.isInteger(count) || count < 1) throw new Error(`${path}: "${line}" is not a count, a tab and a key`);
        lines.set(line.slice(tab + 1), count);
    }
    return { path, lines };
}

/** One User's window is the User window's: the burn-down counts the element, not who it was opened on. */
const kindOf = (id: string): string => id.startsWith('user-element-') ? 'user-element' : id;

/** A finding without its numbers: a width a node's data moves is the same finding. */
const signature = (line: string): string => line.split('').filter((c) => c < '0' || c > '9').join('');

test('every element, live, can be reached whole', async ({ page }) => {
    expect(API, 'QNTX_GATE_API names no node: the live gate has nothing to ask').not.toBe('');
    expect(TOKEN, 'QNTX_GATE_TOKEN is empty: the live gate has no user').not.toBe('');
    expect(ORIGIN, 'QNTX_GATE_ORIGIN names no web origin: the node\'s socket answers none').not.toBe('');
    test.setTimeout(15 * 60_000);

    const wire: Wire = { inflight: 0, answered: 0, refused: [] };
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
    const project = test.info().project.name;
    // What this project owes WCAG 2.2: each key's distinct findings.
    const owed = new Map<string, Set<string>>();
    const owe = (criterion: string, id: string, lines: string[]) => {
        for (const line of lines) {
            const key = `${project} ${kindOf(id)} ${criterion}`;
            owed.set(key, (owed.get(key) ?? new Set<string>()).add(signature(line)));
        }
    };
    // Every element the tray offers, then the User window on every User the node lists, as Users opens it:
    // a full DID or a long email is one User's, not the first's.
    const opens: Array<{ id: string; open: () => Promise<void> }> = ids.map((id) => ({
        id, open: async () => { await page.evaluate((id) => window.specimens!.open(id), id); },
    }));
    const users = await page.evaluate(async () =>
        await (await fetch('/auth/users')).json() as Array<{ id: string; display_name: string; level: string; created_at: number }>);
    for (const user of users) {
        opens.push({ id: `user-element-${user.id}`, open: async () => { await page.evaluate((u) => window.specimens!.openUser(u), user); } });
    }
    // Each one may wait out the wire and its opening (QUIET_FOR_MS each), on top of the page standing.
    test.setTimeout(15 * 60_000 + opens.length * 3 * QUIET_FOR_MS);
    for (const { id, open } of opens) {
        console.log(`opening ${id}`);
        try {
            wire.refused = [];
            await open();
            const element = page.locator(`[data-element-id="${id}"]`);
            // As long as the wire is waited on: a simulated iPhone opens an element slower than the default allows.
            await expect.poll(() => element.evaluate((el) => (el as HTMLElement).dataset.form), { message: `${id} is still a dot`, timeout: QUIET_FOR_MS }).not.toBe('dot');
            await expect.poll(() => element.evaluate((el) => el.classList.contains('morphing')), { message: `${id} is still opening`, timeout: QUIET_FOR_MS }).toBe(false);
            // An element that asks the node all the time is never quiet: it is measured once the wait is over.
            await quiet(page, wire);
            // "so, the gate, it can be burndown as well, cant fix all of it now"
            // At 320 CSS px what cannot be reached is 1.4.10 Reflow's; elsewhere it is the gate's own.
            const cut = await unreachable(page, id);
            const held = await misheld(page, id);
            if (project === 'reflow') owe('1.4.10', id, [...cut, ...held]);
            else {
                owe('reach', id, cut);
                owe('opens', id, held);
            }
            owe('2.5.8', id, await undersized(page, id));
            owe('disabled', id, await disabled(page, id));
            // What the node refused while it opened: what that call would have drawn was never measured, so it is not owed.
            const past = [...new Set(wire.refused.map((call) => `the node refused ${call}`))];
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
    expect(users.length, 'the node lists no User: the User window has nobody to open on').toBeGreaterThan(0);
    // As nilcheck holds its baseline: a number up, a key new, a number down or a key gone is said, and the lines as they stand.
    const debt = burnDown();
    const drift: string[] = [];
    for (const [key, findings] of owed) {
        const listed = debt.lines.get(key);
        if (listed === undefined) drift.push(`${key}: ${findings.size}, and it is not in the burn-down: ${[...findings].join(' | ')}`);
        else if (findings.size > listed) drift.push(`${key}: ${findings.size}, more than the burn-down's ${listed}: ${[...findings].join(' | ')}`);
        else if (findings.size < listed) drift.push(`${key}: ${findings.size}, down from the burn-down's ${listed}. Write \`${findings.size}\t${key}\`.`);
    }
    for (const [key, listed] of debt.lines) {
        if (key.startsWith(`${project} `) && !owed.has(key)) drift.push(`${key}: gone, and the burn-down still lists ${listed}. Drop the line.`);
    }
    if (drift.length > 0) console.log(`${debt.path}, ${project}'s lines as they stand:\n${[...owed].map(([key, findings]) => `${findings.size}\t${key}`).sort().join('\n')}`);
    expect.soft(drift, `what is owed on ${project} moved: the burn-down (${debt.path}) only falls`).toEqual([]);
    expect(bad, `${Object.keys(bad).length} of ${opens.length} elements were not measured`).toEqual({});
});
