/**
 * Every element, on a phone and on a desktop, in a real browser: nothing in it
 * is wider than it. The test DOMs lay nothing out, so it is measured here.
 */

// "is the GitHub perfectly respecting of mobile users, YES/NO — if NO ci needs to be RED"
// "i cannot have it regress ever again"
// "if it overflows width, hard fail, die"

import { test, expect, type Page, type Route } from '@playwright/test';
import { readFile } from 'fs/promises';
import { fileURLToPath } from 'url';

/** Every element the tray offers ROOT. A new one is listed here, or the gate is RED. */
const ELEMENTS = [
    'canvas-workspace',
    'database-element',
    'sigma-panel',
    'embeddings-element',
    'i-element',
    'am-element',
    'tokens-element',
    'users-element',
    'roles-element',
    'market-element',
    'mail-element',
    'github-element',
    'approvals-element',
    'obsidian-element',
    'ground-element',
    'claude-element',
    'pi-element',
    'agent-element',
    'pulse-element',
    'plugin-element',
    'handlers-element',
    'parity-element',
    'llm-provider-element',
];

/** The User the fixtures hold, opened as the User window (user-element.ts). */
const TIM = { id: 'US-TIM-1', display_name: 'Tim de Facile', level: 'ATTESTOR', created_at: 2 };

const FIXTURES = fileURLToPath(new URL('fixtures/', import.meta.url));

/** The page's own files, served by serve.ts; everything else is the node's, answered here. */
function isStatic(pathname: string): boolean {
    return pathname === '/' || pathname === '/index.html' || pathname === '/qntx.jpg'
        || pathname.startsWith('/js/') || pathname.startsWith('/css/') || pathname.startsWith('/fonts/');
}

/** The file that answers a GET: the path under fixtures/, the query dropped, a directory's index. */
function fixtureOf(pathname: string): string {
    const bare = pathname.endsWith('/') ? `${pathname}index` : pathname;
    return `${FIXTURES}${bare.slice(1)}${bare.endsWith('.json') ? '' : '.json'}`;
}

/** What went over the wire: calls nothing answered, calls in flight, calls answered. */
interface Wire {
    missing: string[];
    inflight: number;
    answered: number;
}

/** Answers one call from fixtures. `/i/reaches` is answered with every path asked: the page is ROOT's. */
async function answer(route: Route, wire: Wire): Promise<void> {
    const url = new URL(route.request().url());
    const method = route.request().method();
    const call = `${method} ${url.pathname}${url.search}`;
    wire.inflight += 1;
    try {
        if (url.pathname === '/i/reaches') {
            const asked = (url.searchParams.get('paths') ?? '').split(',').filter((path) => path !== '');
            await route.fulfill({ json: { paths: asked } });
            return;
        }
        if (method !== 'GET') {
            wire.missing.push(call);
            await route.fulfill({ status: 405, body: `${call}: the gate answers GETs from fixtures, and an element opening makes no other call` });
            return;
        }
        const file = fixtureOf(url.pathname);
        let body: string;
        try {
            body = await readFile(file, 'utf8');
        } catch (err: unknown) {
            wire.missing.push(`${call} (no fixture at ${file})`);
            await route.fulfill({ status: 404, body: `${call}: no fixture at ${file}: ${err instanceof Error ? err.message : String(err)}` });
            return;
        }
        await route.fulfill({ status: 200, contentType: 'application/json', body });
    } finally {
        wire.inflight -= 1;
        wire.answered += 1;
    }
}

/** Nothing on the wire across two frames: an element done asking has drawn what it got. */
async function quiet(page: Page, wire: Wire): Promise<void> {
    await expect.poll(async () => {
        const before = wire.answered;
        await page.evaluate(() => new Promise<void>((done) => {
            requestAnimationFrame(() => requestAnimationFrame(() => done()));
        }));
        return wire.inflight === 0 && wire.answered === before;
    }, { message: 'the element is still asking the node' }).toBe(true);
}

/** Opens one element and waits until it is a window, still, with nothing on the wire. */
async function opened(page: Page, id: string, wire: Wire): Promise<void> {
    await page.evaluate((id) => window.specimens!.open(id), id);
    await settled(page, id, wire);
}

async function settled(page: Page, id: string, wire: Wire): Promise<void> {
    const element = page.locator(`[data-element-id="${id}"]`);
    // A window on a desktop, a panel on a phone, the workspace for the canvas: anything but the dot it was.
    await expect.poll(() => element.evaluate((el) => (el as HTMLElement).dataset.form), { message: `${id} is still a dot` }).not.toBe('dot');
    await expect.poll(() => element.evaluate((el) => el.classList.contains('morphing'))).toBe(false);
    await quiet(page, wire);
    // An element drawing a refusal is narrow, and a narrow refusal proves nothing about the element: the fixture is wrong.
    const refused = await element.locator('.element-error, .element-loading').evaluateAll((boxes) => boxes
        .map((box) => box.textContent ?? '')
        .filter((text, i) => boxes[i]!.classList.contains('element-error') || text.includes('oading')));
    expect(refused, `${id} drew what the fixtures answered as a refusal, or is still loading`).toEqual([]);
}

/** The element stands whole on its screen: both edges, measured here, whatever Elements' version checks. */
async function onScreen(page: Page, id: string): Promise<void> {
    const edges = await page.locator(`[data-element-id="${id}"]`).evaluate((el) => {
        const r = el.getBoundingClientRect();
        return { left: Math.round(r.left), right: Math.round(r.right), screen: window.innerWidth };
    });
    expect(edges.left, `${id} starts left of its screen: ${JSON.stringify(edges)}`).toBeGreaterThanOrEqual(-1);
    expect(edges.right, `${id} ends right of its screen: ${JSON.stringify(edges)}`).toBeLessThanOrEqual(edges.screen + 1);
}

/** Every node of the element stands inside it, or is named with how far it reaches past. */
async function inside(page: Page, id: string): Promise<void> {
    const past = await page.locator(`[data-element-id="${id}"]`).evaluate((element) => {
        const edge = element.getBoundingClientRect();
        const reaching = Array.from(element.querySelectorAll<HTMLElement>('*')).filter((node) => {
            const r = node.getBoundingClientRect();
            if (r.width === 0 && r.height === 0) return false;
            return r.left < edge.left - 1 || r.right > edge.right + 1 || node.scrollWidth > node.clientWidth + 1;
        });
        const innermost = reaching.filter((node) => !reaching.some((other) => other !== node && node.contains(other)));
        const dead = element.querySelector('[data-too-wide]');
        return [
            ...innermost.map((node) => {
                const r = node.getBoundingClientRect();
                const text = (node.textContent ?? '').trim().slice(0, 32);
                return `${node.tagName.toLowerCase()}${node.className ? `.${String(node.className).split(' ').join('.')}` : ''} "${text}" ${Math.round(r.left - edge.left)}..${Math.round(r.right - edge.left)} in ${Math.round(edge.width)}, scrolls ${node.scrollWidth} in ${node.clientWidth}`;
            }),
            ...(dead ? [`dropped dead: ${(dead as HTMLElement).dataset.tooWide}`] : []),
        ];
    });
    expect(past, `${id} has content that is not reachable`).toEqual([]);
}

test.describe('Nothing in an element is wider than it', () => {
    let wire: Wire;

    test.beforeAll(async ({ request }) => {
        // serve.ts bundles the page on request; a bundle that did not build is said here, not as a page that never stands.
        const built = await request.get('/js/specimens.js');
        expect(built.ok(), await built.text()).toBe(true);
    });

    test.beforeEach(async ({ page }) => {
        wire = { missing: [], inflight: 0, answered: 0 };
        await page.route((url) => !isStatic(url.pathname), (route) => answer(route, wire));
        await page.goto('/');
        await page.waitForFunction(() => window.specimens !== undefined);
    });

    test('the tray offers these elements, and no other', async ({ page }) => {
        const ids = await page.evaluate(() => window.specimens!.ids());
        expect([...ids].sort()).toEqual([...ELEMENTS].sort());
    });

    for (const id of ELEMENTS) {
        test(id, async ({ page }) => {
            await opened(page, id, wire);
            expect(wire.missing, 'every call an element makes on opening is answered from browser/fixtures').toEqual([]);
            await page.evaluate((id) => window.specimens!.fits(id), id);
            await onScreen(page, id);
            await inside(page, id);
        });
    }

    test('the User window', async ({ page }) => {
        await page.evaluate((u) => window.specimens!.openUser(u), TIM);
        await settled(page, `user-element-${TIM.id}`, wire);
        expect(wire.missing, 'every call the User window makes on opening is answered from browser/fixtures').toEqual([]);
        await page.evaluate((id) => window.specimens!.fits(id), `user-element-${TIM.id}`);
        await onScreen(page, `user-element-${TIM.id}`);
        await inside(page, `user-element-${TIM.id}`);
    });
});

// "i cant even use the mail element on desktop"
// The fixtures answer every call, so a refusal was never drawn here: the Mail window grew past the
// screen on what the node refused it, which no fixture said. Here the node refuses everything.

/** What a node in trouble answers: a 500 whose body is long, one line, and unbroken by spaces for long stretches. */
const REFUSAL = JSON.stringify({
    id: 'ERR-1792245572313-2',
    error: 'the mail templates could not be read: template record AS-CLEANAPI-MAILTEMP-CLEANAPI-XEV3IP7S was kept before a template\'s bodies were named: the plugin sets it again',
    detail: Array.from({ length: 16 }, (_, i) => `github.com/teranos/QNTX/server/handlers.go:${100 + i}`).join(','),
});

test.describe('Nothing in an element is wider than it, or than its screen, when the node refuses everything', () => {
    let wire: Wire;
    // The page stands on its fixtures; from then on, every element it opens is refused.
    let refusing: boolean;

    test.beforeEach(async ({ page }) => {
        wire = { missing: [], inflight: 0, answered: 0 };
        refusing = false;
        await page.route((url) => !isStatic(url.pathname), async (route) => {
            if (!refusing) {
                await answer(route, wire);
                return;
            }
            const url = new URL(route.request().url());
            wire.inflight += 1;
            try {
                if (url.pathname === '/i/reaches') {
                    const asked = (url.searchParams.get('paths') ?? '').split(',').filter((path) => path !== '');
                    await route.fulfill({ json: { paths: asked } });
                    return;
                }
                await route.fulfill({ status: 500, contentType: 'application/json', body: REFUSAL });
            } finally {
                wire.inflight -= 1;
                wire.answered += 1;
            }
        });
        await page.goto('/');
        await page.waitForFunction(() => window.specimens !== undefined);
        await quiet(page, wire);
        refusing = true;
    });

    for (const id of ELEMENTS) {
        test(`${id}, refused`, async ({ page }) => {
            await page.evaluate((id) => window.specimens!.open(id), id);
            const element = page.locator(`[data-element-id="${id}"]`);
            await expect.poll(() => element.evaluate((el) => (el as HTMLElement).dataset.form), { message: `${id} is still a dot` }).not.toBe('dot');
            await expect.poll(() => element.evaluate((el) => el.classList.contains('morphing'))).toBe(false);
            await quiet(page, wire);
            await page.evaluate((id) => window.specimens!.fits(id), id);
            await onScreen(page, id);
            await inside(page, id);
        });
    }
});

// "AND MAIL IS AN OFFENDING ELEMENT, SO THE CI SHOULD POINT IT OUT"
// "IF AN ELEMENT REGRESSES IT SHOULD FAIL"
// Nobody picks what the node says here: every word it answers with is long.

/** Unbroken by spaces, as an id, a hash or a path is. */
const UNBROKEN = 'AS-CLEANAPI-MAILTEMP-CLEANAPI-XEV3IP7S'.repeat(4);

/** A word the page reads as a word, not a key: an id, a level, a time or a path stays as the fixture has it. */
function lengthens(word: string): boolean {
    return word.includes(' ') && !word.startsWith('/') && !word.startsWith('http') && Number.isNaN(Date.parse(word));
}

function lengthened(value: unknown): unknown {
    if (typeof value === 'string') return lengthens(value) ? `${value} ${UNBROKEN}` : value;
    if (Array.isArray(value)) return value.map(lengthened);
    if (value !== null && typeof value === 'object') {
        return Object.fromEntries(Object.entries(value).map(([key, inner]) => [key, lengthened(inner)]));
    }
    return value;
}

test.describe('Nothing in an element is wider than it when every word the node says is long', () => {
    let wire: Wire;

    test.beforeEach(async ({ page }) => {
        wire = { missing: [], inflight: 0, answered: 0 };
        await page.route((url) => !isStatic(url.pathname), async (route) => {
            const url = new URL(route.request().url());
            if (url.pathname === '/i/reaches' || route.request().method() !== 'GET') {
                await answer(route, wire);
                return;
            }
            wire.inflight += 1;
            try {
                const file = fixtureOf(url.pathname);
                let body: string;
                try {
                    body = await readFile(file, 'utf8');
                } catch (err: unknown) {
                    wire.missing.push(`GET ${url.pathname} (no fixture at ${file})`);
                    await route.fulfill({ status: 404, body: `no fixture at ${file}: ${err instanceof Error ? err.message : String(err)}` });
                    return;
                }
                await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(lengthened(JSON.parse(body))) });
            } finally {
                wire.inflight -= 1;
                wire.answered += 1;
            }
        });
        await page.goto('/');
        await page.waitForFunction(() => window.specimens !== undefined);
    });

    for (const id of ELEMENTS) {
        test(`${id}, long`, async ({ page }) => {
            await page.evaluate((id) => window.specimens!.open(id), id);
            const element = page.locator(`[data-element-id="${id}"]`);
            await expect.poll(() => element.evaluate((el) => (el as HTMLElement).dataset.form), { message: `${id} is still a dot` }).not.toBe('dot');
            await expect.poll(() => element.evaluate((el) => el.classList.contains('morphing'))).toBe(false);
            await quiet(page, wire);
            await onScreen(page, id);
            await inside(page, id);
        });
    }
});
