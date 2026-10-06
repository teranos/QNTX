/**
 * @jest-environment jsdom
 *
 * Claude element — the ROOT agent: said to, and read as the session it continues.
 *
 * Personas: Tim (happy path), Spike (edge cases), Jenny (complex scenarios).
 */

import { describe, test, expect } from 'bun:test';
import { drawClaude, type ClaudeAm, type ClaudeSaid, type RootAgent } from './claude-element';
import type { TranscriptRead } from './components/element/transcript-element';

const USE_JSDOM = process.env.USE_JSDOM === '1';

const MODES = ['acceptEdits', 'auto', 'bypassPermissions', 'manual', 'dontAsk', 'plan'];

const am: ClaudeAm = {
    did: 'did:key:z6MkqViDuPd5kCy75dWMjEaqwrgQyDCRZhn53yV39ycSaJHB',
    model: 'claude-opus-5-5',
    effort: 'low',
    permission_mode: 'dontAsk',
    permission_modes: MODES,
    allow: ['Bash', 'Read', 'mcp__qntx'],
    session: '',
    answering: false,
    claude_code: '/root/.qntx/claude-code/2.1.289/claude',
    not_ready: '',
};

const SESSION = '2ed899af-e95e-53f2-bf39-e58440f09df4';

const answered: ClaudeSaid = {
    answer: 'Up 3 days.',
    is_error: false,
    subtype: 'success',
    session: SESSION,
    model: 'claude-opus-5-5',
    claude_code: '2.1.289',
    permission_mode: 'dontAsk',
    denied: [],
    cost_usd: 0.05,
    took_ms: 2592,
    unwritten: '',
};

const session: TranscriptRead = {
    session: SESSION,
    subjects: ['qntx/root-agent'],
    started: '2026-10-05T09:00:00Z',
    ended: '2026-10-05T09:00:04Z',
    folded: 0,
    model: 'claude-opus-5-5',
    effort: '',
    turns: [
        { at: '2026-10-05T09:00:00Z', speaker: 'human', text: 'how long has the box been up?', of: 'p-1' },
        { at: '2026-10-05T09:00:02Z', speaker: 'tool', text: 'uptime', of: 't-2' },
        { at: '2026-10-05T09:00:04Z', speaker: 'assistant', text: 'Up 3 days.', of: 's-3' },
    ],
};

// A node standing in for the one the element asks, keeping what it was sent.
function node(over: Partial<RootAgent> = {}, is: ClaudeAm = am): RootAgent & { sent: Array<[string, string]> } {
    const sent: Array<[string, string]> = [];
    return {
        sent,
        am: () => Promise.resolve(is),
        say: (says, mode) => { sent.push([says, mode]); return Promise.resolve(answered); },
        read: () => Promise.resolve(session),
        ...over,
    };
}

// Everything already settled has been drawn.
async function settled(): Promise<void> {
    for (let i = 0; i < 5; i++) await new Promise(resolve => setTimeout(resolve, 0));
}

function say(body: HTMLElement, words: string): void {
    body.querySelector<HTMLTextAreaElement>('.claude-says')!.value = words;
    body.querySelector<HTMLButtonElement>('.claude-send')!.click();
}

describe('Claude - Tim', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    // Tim: opening it says who answers, before anything is said.
    test('it says who it is and how it runs', async () => {
        const body = document.createElement('div');
        const stop = drawClaude(body, node());
        await settled();
        const who = body.querySelector('.claude-who')?.textContent ?? '';
        expect(who).toContain('did:key:z6MkqViDuPd5');
        expect(who).toContain('claude-opus-5-5');
        expect(who).toContain('low');
        stop();
    });

    // "make sure --permission-mode is configurable in the Claude Element"
    test('every permission mode is offered, and the one am.toml gives is chosen', async () => {
        const body = document.createElement('div');
        const stop = drawClaude(body, node());
        await settled();
        const mode = body.querySelector<HTMLSelectElement>('.claude-mode')!;
        expect([...mode.options].map(o => o.value)).toEqual(MODES);
        expect(mode.value).toBe('dontAsk');
        stop();
    });

    // Tim: what he says is sent in the mode he chose, and the session is drawn when it answers.
    test('what is said is sent in the mode chosen, and its session is read', async () => {
        const body = document.createElement('div');
        const asked = node();
        const stop = drawClaude(body, asked);
        await settled();
        body.querySelector<HTMLSelectElement>('.claude-mode')!.value = 'plan';
        say(body, 'how long has the box been up?');
        await settled();

        expect(asked.sent).toEqual([['how long has the box been up?', 'plan']]);
        const rows = [...body.querySelectorAll('.tr-turn')].map(t => t.querySelector('.tr-text')?.textContent?.trim());
        expect(rows).toEqual(['how long has the box been up?', 'Up 3 days.']);
        expect(body.querySelector<HTMLTextAreaElement>('.claude-says')!.value).toBe('');
        stop();
    });

    // Tim: tomorrow it is the same session, drawn on opening.
    test('a session it continues is drawn on opening', async () => {
        const body = document.createElement('div');
        const stop = drawClaude(body, node({}, { ...am, session: SESSION }));
        await settled();
        expect(body.querySelectorAll('.tr-turn')).toHaveLength(2);
        stop();
    });
});

describe('Claude - Spike', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    // Spike: nothing typed is nothing said.
    test('an empty box sends nothing', async () => {
        const body = document.createElement('div');
        const asked = node();
        const stop = drawClaude(body, asked);
        await settled();
        say(body, '   ');
        await settled();
        expect(asked.sent).toEqual([]);
        stop();
    });

    // Spike: a refusal is said in the words it came in, and what he typed is still there.
    test('a refusal is drawn, and the words are kept', async () => {
        const body = document.createElement('div');
        const stop = drawClaude(body, node({
            say: () => Promise.reject(new Error('/api/claude/say: HTTP 500 Claude Code did not answer: exit status 1')),
        }));
        await settled();
        say(body, 'install htop');
        await settled();
        // Through the one render an error has, which holds what it was handed.
        const drawn = body.querySelector<HTMLElement & { error: { title: string; why: string } | null }>('.claude-failed sacred-error');
        expect(`${drawn?.error?.title}: ${drawn?.error?.why}`).toContain('Claude Code did not answer: exit status 1');
        expect(body.querySelector<HTMLTextAreaElement>('.claude-says')!.value).toBe('install htop');
        expect(body.querySelector<HTMLButtonElement>('.claude-send')!.disabled).toBe(false);
        stop();
    });

    // Spike: a tool it reached for and was not allowed is said with the answer.
    test('what it was not allowed is said', async () => {
        const body = document.createElement('div');
        const stop = drawClaude(body, node({
            say: () => Promise.resolve({ ...answered, denied: ['Write', 'WebFetch'] }),
        }));
        await settled();
        say(body, 'write it down');
        await settled();
        const denied = body.querySelector('.claude-denied')?.textContent ?? '';
        expect(denied).toContain('Write');
        expect(denied).toContain('WebFetch');
        stop();
    });

    // Spike: a node with no ROOT agent says so, in the node's words.
    test('a node with no ROOT agent says why', async () => {
        const body = document.createElement('div');
        const stop = drawClaude(body, node({
            am: () => Promise.reject(new Error("/api/claude: HTTP 404 this node's am.toml names no ROOT agent")),
        }));
        await settled();
        expect(body.querySelector('.claude-who')?.textContent).toContain('names no ROOT agent');
        expect(body.querySelector<HTMLButtonElement>('.claude-send')!.disabled).toBe(true);
        stop();
    });

    // Spike: Claude Code still being fetched is said, and saying is still possible.
    test('why it is not ready is said', async () => {
        const body = document.createElement('div');
        const stop = drawClaude(body, node({}, { ...am, claude_code: '', not_ready: 'Claude Code is still being fetched' }));
        await settled();
        expect(body.querySelector('.claude-who')?.textContent).toContain('Claude Code is still being fetched');
        stop();
    });
});

describe('Claude - Jenny', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    // Jenny: opened while it answers somebody else, it draws that turn as it goes.
    test('a turn already going is followed', async () => {
        const body = document.createElement('div');
        let reads = 0;
        const stop = drawClaude(body, node(
            { read: () => { reads++; return Promise.resolve(session); } },
            { ...am, session: SESSION, answering: true },
        ), 1);
        await settled();
        await new Promise(resolve => setTimeout(resolve, 20));
        expect(reads).toBeGreaterThan(1);
        expect(body.querySelector('.claude-status')?.textContent).toContain('answering');
        stop();
    });

    // Jenny: while it answers, a second thing cannot be said from here.
    test('the box is held while it answers', async () => {
        const body = document.createElement('div');
        let answer: (said: ClaudeSaid) => void = () => {};
        const asked = node({ say: () => new Promise<ClaudeSaid>(resolve => { answer = resolve; }) });
        const stop = drawClaude(body, asked, 1);
        await settled();
        say(body, 'first');
        await settled();
        expect(body.querySelector<HTMLButtonElement>('.claude-send')!.disabled).toBe(true);
        answer(answered);
        await settled();
        expect(body.querySelector<HTMLButtonElement>('.claude-send')!.disabled).toBe(false);
        stop();
    });
});
