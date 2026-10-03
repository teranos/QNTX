/**
 * @jest-environment jsdom
 *
 * Transcript element — one session Ground recorded, read as what was said and done.
 *
 * Personas: Tim (happy path), Spike (edge cases), Jenny (complex scenarios).
 */

import { describe, test, expect } from 'bun:test';
import { renderSessions } from '../../ground-element';
import { renderTranscript, renderAssistant, timeSpacers, type TranscriptRead, type Turn } from './transcript-element';

const USE_JSDOM = process.env.USE_JSDOM === '1';

const read: TranscriptRead = {
    session: '2ed899af-e95e-53f2-bf39-e58440f09df4',
    subjects: ['user/QNTX:datapunt-owns-the-reference'],
    started: '2026-10-01T22:00:00Z',
    ended: '2026-10-02T01:00:06Z',
    folded: 0,
    model: 'claude-opus-5-5',
    effort: 'xhigh',
    turns: [
        { at: '2026-10-01T22:00:00Z', speaker: 'human', text: 'Build QNTX here', of: 'p-1' },
        { at: '2026-10-01T22:00:01Z', speaker: 'tool', text: 'make cli', of: 't-2' },
        { at: '2026-10-01T22:00:02Z', speaker: 'ground', text: 'no-comment-blocks on PreToolUse', of: 'g-3' },
        { at: '2026-10-02T01:00:06Z', speaker: 'assistant', text: 'QNTX **builds** here.', of: 's-4' },
    ],
};

// A run of small turns between two things said, in one minute.
function turnsOf(...spoken: Array<[string, string]>): Turn[] {
    return spoken.map(([speaker, text], i) => ({
        at: `2026-10-03T12:00:${String(i).padStart(2, '0')}Z`, speaker, text, of: `a-${i}`,
    }));
}

function session(turns: Turn[]): TranscriptRead {
    return { ...read, turns };
}

function chips(body: HTMLElement): string[][] {
    return [...body.querySelectorAll('.tr-chips')].map(line =>
        [...line.querySelectorAll('.tr-chip')].map(c => c.textContent ?? ''));
}

function rightPress(el: HTMLElement): MouseEvent {
    const e = new MouseEvent('contextmenu', { bubbles: true, cancelable: true });
    el.dispatchEvent(e);
    return e;
}

const mixed = turnsOf(
    ['read', '/a.go'], ['read', '/b.go'],
    ['tool', 'make cli'], ['tool', 'make test'], ['tool', 'git push'],
    ['ground', 'no-comment-blocks on PreToolUse'],
    ['read', '/c.go'], ['read', '/d.go'],
);

describe('Transcript - Tim', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    // Tim: what was said is a row; what was done between is one line of chips.
    test('a session reads as its turns, in order', () => {
        const body = document.createElement('div');
        renderTranscript(body, read, () => {});
        const rows = [...body.querySelectorAll('.tr-turn')].map(t => t.querySelector('.tr-speaker')?.textContent);
        expect(rows).toEqual(['[human]', '[assistant]']);
        expect(chips(body)).toEqual([['[tool]', '[ground]']]);
        expect(body.querySelector('.tr-warp')).not.toBeNull();
    });

    // Tim: runs of one speaker are one chip, and mixed runs share a line.
    // "[read] 2x [tool] 3x [ground] [read] 2x"
    test('runs of one speaker are one chip, and mixed runs share a line', () => {
        const body = document.createElement('div');
        renderTranscript(body, session(mixed), () => {});
        expect(chips(body)).toEqual([['[read] 2x', '[tool] 3x', '[ground]', '[read] 2x']]);
    });

    // Tim: a press selects, a right press unselects, a press on the selected copies.
    // "left adds to selection and right removes it left on already selected adds to clipboard what is selected"
    test('left selects, right unselects, left on the selected copies the selection', () => {
        const body = document.createElement('div');
        const copied: string[] = [];
        renderTranscript(body, read, (text) => copied.push(text));
        const human = body.querySelector<HTMLElement>('[data-of="p-1"]')!;
        const assistant = body.querySelector<HTMLElement>('[data-of="s-4"]')!;

        assistant.click();
        human.click();
        expect(human.classList.contains('tr-selected')).toBe(true);
        expect(copied).toEqual([]);

        human.click();
        expect(copied).toEqual(['[human] Build QNTX here\n[assistant] QNTX **builds** here.']);

        rightPress(human);
        expect(human.classList.contains('tr-selected')).toBe(false);
        assistant.click();
        expect(copied[1]).toBe('[assistant] QNTX **builds** here.');
    });

    // Tim: three hours between two turns is three blocks of space.
    test('the gap between turns is drawn', () => {
        const body = document.createElement('div');
        renderTranscript(body, read, () => {});
        expect(body.querySelectorAll('.tr-spacer')).toHaveLength(3);
    });

    // Tim: Ground offers the sessions by their first prompt, and one is chosen.
    test('a session is chosen from the list by its first prompt', () => {
        const body = document.createElement('div');
        const chosen: string[] = [];
        renderSessions(body, [read], (id) => chosen.push(id));
        const row = body.querySelector<HTMLElement>('.tr-session')!;
        expect(row.textContent).toContain('Build QNTX here');
        expect(row.querySelector('.tr-ran')?.textContent).toBe('claude-opus-5-5 · xhigh');
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

    // Spike: Ground in a namespace with no session says so.
    test('no sessions is said, not an empty box', () => {
        const body = document.createElement('div');
        renderSessions(body, [], () => {});
        expect(body.textContent).toContain('No session Ground recorded');
    });

    // Spike: what a person said, or a session starting, ends a line of chips.
    test('a human or a session marker ends a line of chips', () => {
        const body = document.createElement('div');
        renderTranscript(body, session(turnsOf(
            ['read', '/a.go'], ['human', 'and then?'], ['read', '/b.go'],
            ['session', 'Start startup'], ['tool', 'ls'],
        )), () => {});
        expect(chips(body)).toEqual([['[read]'], ['[read]'], ['[tool]']]);
    });

    // Spike: a right press is the transcript's, not the browser's menu.
    test('a right press takes no menu and leaves an unselected turn unselected', () => {
        const body = document.createElement('div');
        renderTranscript(body, read, () => {});
        const human = body.querySelector<HTMLElement>('[data-of="p-1"]')!;
        expect(rightPress(human).defaultPrevented).toBe(true);
        expect(human.classList.contains('tr-selected')).toBe(false);
    });
});

describe('Transcript - Jenny', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    // Jenny: a chip is pressed the way a row is, for every turn it holds.
    test('a chip selects and copies every turn it holds', () => {
        const body = document.createElement('div');
        const copied: string[] = [];
        renderTranscript(body, session(mixed), (text) => copied.push(text));
        const tools = body.querySelectorAll<HTMLElement>('.tr-chip')[1];
        tools.click();
        tools.click();
        expect(copied).toEqual(['[tool] make cli\n[tool] make test\n[tool] git push']);
    });

    // Jenny: hovering a chip lists its turns in sequence, each one pressable alone.
    test('hovering a chip lists its turns, and one of them is selected alone', async () => {
        const body = document.createElement('div');
        document.body.appendChild(body);
        const copied: string[] = [];
        renderTranscript(body, session(mixed), (text) => copied.push(text));
        const reads = body.querySelector<HTMLElement>('.tr-chip')!;

        reads.dispatchEvent(new Event('pointerenter'));
        await new Promise(r => setTimeout(r, 350));
        const list = document.querySelector<HTMLElement>('.tr-chip-list')!;
        const entries = [...list.querySelectorAll<HTMLElement>('.tr-turn')];
        expect(entries.map(e => e.textContent)).toEqual(['[read]/a.go', '[read]/b.go']);

        entries[1].click();
        entries[1].click();
        expect(copied).toEqual(['[read] /b.go']);
        expect(reads.classList.contains('tr-selected')).toBe(false);

        reads.dispatchEvent(new Event('pointerleave'));
        await new Promise(r => setTimeout(r, 200));
        expect(document.querySelector('.tr-chip-list')).toBeNull();
        body.remove();
    });
});
