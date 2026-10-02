/**
 * @jest-environment jsdom
 *
 * Transcript element — one session Ground recorded, read as what was said and done.
 *
 * Personas: Tim (happy path), Spike (edge cases), Jenny (complex scenarios).
 */

import { describe, test, expect } from 'bun:test';
import { renderTranscript, renderSessions, renderAssistant, timeSpacers, type TranscriptRead, type Turn } from './transcript-element';

const USE_JSDOM = process.env.USE_JSDOM === '1';

const read: TranscriptRead = {
    session: '2ed899af-e95e-53f2-bf39-e58440f09df4',
    subjects: ['user/QNTX:datapunt-owns-the-reference'],
    started: '2026-10-01T22:00:00Z',
    ended: '2026-10-02T01:00:06Z',
    folded: 0,
    turns: [
        { at: '2026-10-01T22:00:00Z', speaker: 'human', text: 'Build QNTX here', of: 'p-1' },
        { at: '2026-10-01T22:00:01Z', speaker: 'tool', text: 'make cli', of: 't-2' },
        { at: '2026-10-01T22:00:02Z', speaker: 'ground', text: 'no-comment-blocks on PreToolUse', of: 'g-3' },
        { at: '2026-10-02T01:00:06Z', speaker: 'assistant', text: 'QNTX **builds** here.', of: 's-4' },
    ],
};

describe('Transcript - Tim', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    // Tim: the session reads back in order, each turn under its speaker.
    test('a session reads as its turns, in order', () => {
        const body = document.createElement('div');
        renderTranscript(body, read, () => {});
        const turns = [...body.querySelectorAll('.tr-turn')].map(t => t.querySelector('.tr-speaker')?.textContent);
        expect(turns).toEqual(['[human]', '[tool]', '[ground]', '[assistant]']);
        expect(body.querySelector('.tr-warp')).not.toBeNull();
    });

    // Tim: pressing a turn asks for the attestation it was read from.
    test('a pressed turn names its attestation', () => {
        const body = document.createElement('div');
        const opened: Turn[] = [];
        renderTranscript(body, read, (turn) => opened.push(turn));
        body.querySelector<HTMLElement>('[data-of="g-3"]')!.click();
        expect(opened.map(t => t.of)).toEqual(['g-3']);
    });

    // Tim: three hours between two turns is three blocks of space.
    test('the gap between turns is drawn', () => {
        const body = document.createElement('div');
        renderTranscript(body, read, () => {});
        expect(body.querySelectorAll('.tr-spacer')).toHaveLength(3);
    });

    // Tim: the sessions are offered by their first prompt, and one is chosen.
    test('a session is chosen from the list by its first prompt', () => {
        const body = document.createElement('div');
        const chosen: string[] = [];
        renderSessions(body, [read], (id) => chosen.push(id));
        const row = body.querySelector<HTMLElement>('.tr-session')!;
        expect(row.textContent).toContain('Build QNTX here');
        row.click();
        expect(chosen).toEqual([read.session]);
    });
});

describe('Transcript - Spike', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    // Spike: what the assistant said is escaped before its markdown is drawn.
    test('markup in what was said is text, not markup', () => {
        expect(renderAssistant('<b>x</b> **y**')).toBe('&lt;b&gt;x&lt;/b&gt; <b>y</b>\n');
    });

    // Spike: under an hour is no gap, and the blocks stop shrinking at 5px.
    test('spacers start at an hour and stop at 5px', () => {
        expect(timeSpacers(0, 1)).toEqual([]);
        expect(timeSpacers(1, 1 + 59 * 60 * 1000)).toEqual([]);
        expect(timeSpacers(1, 1 + 3 * 60 * 60 * 1000)).toEqual([18, 18, 18]);
        const year = timeSpacers(1, 1 + 365 * 24 * 60 * 60 * 1000);
        expect(Math.min(...year)).toBeGreaterThan(5);
    });

    // Spike: a namespace with no session says so.
    test('no sessions is said, not an empty box', () => {
        const body = document.createElement('div');
        renderSessions(body, [], () => {});
        expect(body.textContent).toContain('No session Ground recorded');
    });
});
