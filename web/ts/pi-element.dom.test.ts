/**
 * @jest-environment jsdom
 *
 * Pi element — the ROOT agent in Pi, drawn as the Claude element draws it.
 */

import { describe, test, expect } from 'bun:test';
import { drawClaude, type RootAgent } from './claude-element';
import { asAm, asSaid, type PiAm, type PiSaid } from './pi-element';
import type { TranscriptRead } from './components/element/transcript-element';

const USE_JSDOM = process.env.USE_JSDOM === '1';

const SESSION = '2ed899af-e95e-53f2-bf39-e58440f09df4';

const piAm: PiAm = {
    did: 'did:key:z6MkqViDuPd5kCy75dWMjEaqwrgQyDCRZhn53yV39ycSaJHB',
    model: 'anthropic/claude-sonnet-4.6',
    thinking: 'low',
    gateway: 'openrouter-qntx',
    session: SESSION,
    answering: false,
    pi: '/nix/store/pi-1.0.3/bin/pi',
    pi_version: '1.0.3',
    not_ready: '',
};

const piSaid: PiSaid = {
    answer: 'Up 3 days.',
    is_error: false,
    stop: 'stop',
    session: SESSION,
    model: 'anthropic/claude-sonnet-4.6',
    pi_version: '1.0.3',
    cost_usd: 0.03,
    took_ms: 4100,
    unwritten: '',
};

const session: TranscriptRead = {
    session: SESSION,
    subjects: ['qntx/root-agent'],
    started: '2026-10-05T09:00:00Z',
    ended: '2026-10-05T09:00:04Z',
    folded: 0,
    model: '',
    effort: 'low',
    turns: [
        { at: '2026-10-05T09:00:00Z', speaker: 'human', text: 'how long has the box been up?', of: 'p-1' },
        { at: '2026-10-05T09:00:01Z', speaker: 'session', text: 'Start pi', of: 's-2' },
        { at: '2026-10-05T09:00:04Z', speaker: 'assistant', text: 'Up 3 days.', of: 's-3' },
    ],
};

async function settled(): Promise<void> {
    for (let i = 0; i < 5; i++) await new Promise(resolve => setTimeout(resolve, 0));
}

describe.skipIf(!USE_JSDOM)('Pi element', () => {
    test('who answers in Pi is drawn, and no permission mode is offered', async () => {
        const body = document.createElement('div');
        const node: RootAgent = {
            am: () => Promise.resolve(asAm(piAm)),
            say: () => Promise.resolve(asSaid(piSaid)),
            read: () => Promise.resolve(session),
        };
        const stop = drawClaude(body, node);
        await settled();

        const who = body.querySelector('.claude-who')!.textContent ?? '';
        expect(who).toContain(piAm.did);
        expect(who).toContain('anthropic/claude-sonnet-4.6');
        expect(who).toContain('low');
        expect(body.querySelector<HTMLSelectElement>('.claude-mode')!.hidden).toBe(true);
        expect(body.textContent).toContain('Start pi');
        stop();
    });

    test('what Pi answered reaches the element in the shape it draws', () => {
        const said = asSaid(piSaid);
        expect(said.answer).toBe('Up 3 days.');
        expect(said.subtype).toBe('stop');
        expect(said.denied).toEqual([]);
        expect(asAm(piAm).permission_modes).toEqual([]);
    });
});
