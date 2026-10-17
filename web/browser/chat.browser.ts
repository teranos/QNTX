/**
 * Claude and Pi as chats, in a real browser, from the fixtures.
 *
 * "claude element should not start at top when opening requiring user to scroll
 * down every single time" and "the inputfield take up the entire bottom of the
 * screen for chat elements like claude or pi" (teranos/elements#40). The package
 * does both for an element that says so: it opens at its end, in the scroller its
 * host declared, and the field it says from has the bottom line to itself.
 *
 * The session is sixty turns here, more than any screen shows.
 *
 * Personas:
 * - Tim: Happy path — Claude and Pi open with what was said last in sight, the field across the bottom
 * - Spike: Edge cases — the transcript is the element's one scroller, declared, and nothing else scrolls
 */

import { test, expect, type Page, type Route } from '@playwright/test';
import { readFile } from 'fs/promises';
import { fileURLToPath } from 'url';

const FIXTURES = fileURLToPath(new URL('fixtures/', import.meta.url));

/** Sixty turns, the last one saying so. */
function longSession(of: 'claude' | 'pi'): unknown {
    const turns = Array.from({ length: 60 }, (_, i) => ({
        at: new Date(Date.UTC(2026, 9, 16, 18, 0, i * 10)).toISOString(),
        speaker: i % 2 ? 'agent' : 'person',
        text: i === 59 ? 'the last thing said' : `turn ${i + 1}, said at some length so that it takes a line or two on a phone`,
        of: i % 2 ? 'agent' : 'person',
    }));
    return { transcript: { session: `${of}-long`, subjects: ['QNTX'], started: turns[0]!.at, ended: turns[59]!.at, turns, folded: 0, model: '', effort: '' } };
}

function isStatic(pathname: string): boolean {
    return pathname === '/' || pathname === '/index.html' || pathname === '/qntx.jpg'
        || pathname.startsWith('/js/') || pathname.startsWith('/css/') || pathname.startsWith('/fonts/');
}

/** The node, from fixtures; its sessions, sixty turns long. */
async function answer(route: Route): Promise<void> {
    const url = new URL(route.request().url());
    if (url.pathname === '/i/reaches') {
        await route.fulfill({ json: { paths: (url.searchParams.get('paths') ?? '').split(',').filter(Boolean) } });
        return;
    }
    if (url.pathname === '/api/claude/session') { await route.fulfill({ json: longSession('claude') }); return; }
    if (url.pathname === '/api/pi/session') { await route.fulfill({ json: longSession('pi') }); return; }
    const bare = url.pathname.endsWith('/') ? `${url.pathname}index` : url.pathname;
    try {
        const body = await readFile(`${FIXTURES}${bare.slice(1)}.json`, 'utf8');
        await route.fulfill({ status: 200, contentType: 'application/json', body });
    } catch {
        await route.fulfill({ status: 404, body: `no fixture for ${url.pathname}` });
    }
}

async function open(page: Page, id: string) {
    const element = page.locator(`[data-element-id="${id}"]`);
    await page.evaluate((id) => window.specimens!.open(id), id);
    await expect.poll(() => element.evaluate((el) => (el as HTMLElement).dataset.form)).not.toBe('dot');
    await expect.poll(() => element.evaluate((el) => el.classList.contains('morphing'))).toBe(false);
    await expect(element.getByText('the last thing said')).toHaveCount(1);
    return element;
}

test.beforeEach(async ({ page }) => {
    await page.route((url) => !isStatic(url.pathname), answer);
    await page.goto('/');
    await page.waitForFunction(() => window.specimens !== undefined);
});

for (const id of ['claude-element', 'pi-element']) {
    test.describe(`Tim: ${id}`, () => {
        test('opens with what was said last in sight', async ({ page }) => {
            const element = await open(page, id);

            await expect(element.getByText('the last thing said')).toBeInViewport();
        });

        test('its field has the bottom line, the whole width', async ({ page }) => {
            const element = await open(page, id);
            const says = element.locator('.claude-says');
            const row = (await element.locator('.claude-say').boundingBox())!;
            const field = (await says.boundingBox())!;

            // As wide as its row, less the row's padding.
            expect(field.width).toBeGreaterThan(row.width - 16);
            expect(field.y + field.height).toBeGreaterThan(row.y + row.height - 8);
        });
    });
}

test.describe('Spike: one scroller', () => {
    test('the transcript is Claude\'s scroller, declared, and nothing else scrolls', async ({ page }) => {
        const element = await open(page, 'claude-element');

        await expect(element.locator('.tr-col')).toHaveAttribute('data-scroller', 'declared');
        await expect(element.locator('[data-scroller="undeclared"]')).toHaveCount(0);
    });
});
