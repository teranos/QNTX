/**
 * Ground element — one picture in cross-section, read from the nebula down.
 *
 * Personas: Tim (happy path), Spike (edge cases), Jenny (complex scenarios).
 */

import { describe, test, expect } from 'bun:test';
import { byPlace, drawGround, openingOf, placeOf, walkOf, type Said } from './ground-element';
import type { TranscriptRead, Turn } from './components/element/transcript-element';

let nth = 0;

function turn(speaker: string, text: string, of = ''): Turn {
    nth++;
    return { at: `2026-10-03T12:00:${String(nth % 60).padStart(2, '0')}Z`, speaker, text, of: of || `a-${nth}` };
}

function session(id: string, subjects: string[], turns: Turn[]): TranscriptRead {
    return {
        session: id, subjects, turns, folded: 0, model: 'claude-opus-5-5', effort: 'xhigh',
        started: '2026-10-03T12:00:00Z', ended: '2026-10-03T13:00:00Z',
    };
}

const held = session('s-1', ['user/QNTX:datapunt-owns-the-reference', 'user/QNTX:main', 'ground'], [
    turn('human', 'Build QNTX here'),
    turn('tool', 'make cli'),
    turn('ground', 'no-comment-blocks on PreToolUse'),
    turn('assistant', 'QNTX builds here.'),
]);

const walked = session('s-2', ['grove', 'user/QNTX:datapunt-owns-the-reference', 'ground'], [
    turn('human', 'Performing ritual grove, rite 1 of 3: built. Ground runs this rite itself: make cli. Nothing is asked of you'),
    turn('rite', 'built advance 0', 'ritual:rite:grove-1:built:6'),
    turn('rite', 'tested hold 1', 'ritual:rite:grove-1:tested:7'),
]);

const row: Said[] = [
    { name: 'QNTX', note: 'SUPER', symbol: '+' },
    { name: 'ci', note: 'watching datapunt-owns-the-reference 4 runs', symbol: '+' },
    { name: 'up', note: '3d4h', symbol: '+' },
    { name: 'datapunt', note: '0.3.7', symbol: '!' },
];

function names(body: HTMLElement): string[] {
    return [...body.querySelectorAll('.gr-name b')].map(name => name.textContent ?? '');
}

function drawn(): { body: HTMLElement; scene: ReturnType<typeof drawGround> } {
    const body = document.createElement('div');
    return { body, scene: drawGround(body) };
}

describe('Ground - Tim', () => {
    // "THE ELEMENT NEEDS TO SHOW THESE IN ORDER"
    test('the picture is drawn in order, from the nebula down', () => {
        const { body } = drawn();
        expect(names(body)).toEqual(['Scry', 'Stars', 'Comet', 'Sky', 'Ground', 'Underground', 'Rituals and rites', 'Deeper']);
    });

    // Tim: every stratum is drawn as something, before anything is read.
    test('every stratum has its plate', () => {
        const { body } = drawn();
        for (const stratum of body.querySelectorAll('.gr-stratum')) {
            expect(stratum.querySelector('svg.gr-plate')).not.toBeNull();
        }
    });

    // Tim: a session a person held is under its place, named by what they first said.
    test('a session is under its place, with the branches it touched', () => {
        const { body, scene } = drawn();
        scene.read([held], () => {});
        const place = body.querySelector('.gr-deep .gr-place')!;
        expect(place.querySelector('.gr-place-name b')?.textContent).toBe('user/QNTX');
        expect(place.querySelector('.tr-opening')?.textContent).toBe('Build QNTX here');
        expect([...place.querySelectorAll('.gr-under-line .gr-chip')].map(c => c.textContent)).toEqual(['datapunt-owns-the-reference', 'main']);
    });

    // Tim: pressing a session chooses it.
    test('a session pressed is the one chosen', () => {
        const { body, scene } = drawn();
        const chosen: string[] = [];
        scene.read([held, walked], id => chosen.push(id));
        body.querySelector<HTMLElement>('.gr-deep .tr-session')!.click();
        body.querySelector<HTMLElement>('.gr-rites .tr-session')!.click();
        expect(chosen).toEqual(['s-1', 's-2']);
    });

    // Tim: what the node says of itself hangs as stars, each with its words.
    test('what the node says hangs as stars', () => {
        const { body, scene } = drawn();
        scene.said(row);
        const hung = [...body.querySelectorAll('.gr-hung .gr-said')].map(s => s.querySelector('.gr-fact-name')?.textContent);
        expect(hung).toEqual(['QNTX', 'up', 'datapunt']);
        expect(body.querySelector('.gr-hung .gr-unwell .gr-fact-name')?.textContent).toBe('datapunt');
    });
});

describe('Ground - Spike', () => {
    // "IN THE REAL UI IT WILL BE CROSSHATCHED OUT, EACH NOTHING THEIR LIMITATION WITH REGARDS TO WHAT GROUND OR SKY DOES"
    test('what is not real yet is hatched, and says its limitation', () => {
        const { body } = drawn();
        const unreal = [...body.querySelectorAll('.gr-unreal')];
        expect(unreal.map(s => s.querySelector('.gr-name b')?.textContent)).toEqual(['Scry', 'Comet', 'Underground']);
        for (const stratum of unreal) {
            expect(stratum.querySelector('.gr-limit')?.textContent).not.toBe('');
        }
    });

    // Spike: every stratum that is real in part still says what it cannot.
    test('every stratum carries a limitation', () => {
        const { body } = drawn();
        for (const stratum of body.querySelectorAll('.gr-stratum')) {
            expect(stratum.querySelector('.gr-limit')).not.toBeNull();
        }
    });

    // "Scry should be the strata at the very top the nebula"
    test('scry is hatched until the row names it', () => {
        const { body, scene } = drawn();
        const scry = body.querySelector('.gr-scry')!;
        scene.said(row);
        expect(scry.classList.contains('gr-unreal')).toBe(true);
        expect(scry.querySelector('.gr-limit')?.textContent).toContain('names no scry');
        scene.said([...row, { name: 'scry', note: '0.4.2', symbol: '+' }]);
        expect(scry.classList.contains('gr-unreal')).toBe(false);
        expect(scry.querySelector('.gr-told')?.textContent).toBe('This node runs scry 0.4.2.');
    });

    // Spike: a long thing said is written under the stars, and news is too.
    test('what takes long to say is written, not hung', () => {
        const { body, scene } = drawn();
        scene.said([...row, { id: 'immediate:ci-status:1', name: 'ci', note: 'success', symbol: '+' }]);
        const written = [...body.querySelectorAll('.gr-written .gr-said')].map(s => s.querySelector('.gr-fact-note')?.textContent);
        expect(written).toEqual(['watching datapunt-owns-the-reference 4 runs', 'success']);
    });

    // Spike: a node that did not say its row leaves no star of the last one hanging.
    test('no star outlives its row', () => {
        const { body, scene } = drawn();
        scene.said(row);
        scene.unsaid('/am/statusline?format=json: HTTP 502');
        expect(body.querySelectorAll('.gr-said')).toHaveLength(0);
        expect(body.querySelector('.gr-stars')?.textContent).toContain('HTTP 502');
    });

    // Spike: what Claude Code hands a session is not what a person said in it.
    test('a prompt handed to a session is not its opening', () => {
        const handed = session('s-3', ['user/QNTX:main'], [
            turn('human', '<task-notification>\n<task-id>t-1</task-id>\n</task-notification>'),
            turn('human', '<agent-message from="a-1">done</agent-message>'),
            turn('human', '\n\n<pasted_content id="p-1">\n and then?\n  the second line\n</pasted_content id="p-1">'),
        ]);
        expect(openingOf(handed)).toBe('and then?');
        expect(openingOf(walked)).toBe('');
    });

    // Spike: Ground writes unknown where it read no branch, and a rite's subject is no place.
    test('unknown is no branch, and a ritual is no place', () => {
        const read = session('s-4', ['grove', 'user/QNTX:unknown', 'user/QNTX:main', 'user/ground:main'], []);
        expect(placeOf(read)).toEqual({ place: 'user/QNTX', branches: ['main'] });
        expect(placeOf(session('s-5', ['ground'], []))).toEqual({ place: '', branches: [] });
    });

    // Spike: the read failing is said where the sessions would be.
    test('a read that failed is said', () => {
        const { body, scene } = drawn();
        scene.unread('/api/transcripts: HTTP 403');
        expect(body.querySelector('.gr-deep')?.textContent).toContain('HTTP 403');
    });
});

describe('Ground - Jenny', () => {
    // Jenny: a thing said again is the star it already was, a thing no longer said is gone.
    test('a star said again is the same star', () => {
        const { body, scene } = drawn();
        scene.said(row);
        const up = [...body.querySelectorAll<HTMLElement>('.gr-hung .gr-said')][1];
        scene.said([row[0], { name: 'up', note: '3d5h', symbol: '+' }]);
        const hung = [...body.querySelectorAll<HTMLElement>('.gr-hung .gr-said')];
        expect(hung).toHaveLength(2);
        expect(hung[1]).toBe(up);
        expect(up.querySelector('.gr-fact-note')?.textContent).toBe('3d5h');
        expect(body.querySelectorAll('.gr-written .gr-said')).toHaveLength(0);
    });

    // Jenny: a session a ritual walked is drawn by its rites, and is not among the sessions a person held.
    test('a walked session is a performance, one tile per rite', () => {
        const { body, scene } = drawn();
        scene.read([held, walked], () => {});
        expect(walkOf(walked)).toBe('grove');
        expect(walkOf(held)).toBeNull();
        const walk = body.querySelector('.gr-rites .gr-walk')!;
        expect(walk.querySelector('.gr-walk-head b')?.textContent).toBe('grove');
        expect([...walk.querySelectorAll('.gr-tile')].map(t => t.className)).toEqual(['gr-tile gr-verdict-advance', 'gr-tile gr-verdict-hold']);
        expect(walk.textContent).toContain('reached tested hold 1');
        expect(body.querySelectorAll('.gr-deep .tr-session')).toHaveLength(1);
    });

    // Jenny: places come in the order of their newest session, and hold every session of theirs.
    test('sessions of one place are together', () => {
        const other = session('s-6', ['user/ground:main'], [turn('human', 'hey')]);
        const again = session('s-7', ['user/QNTX:main'], [turn('human', 'and then?')]);
        expect(byPlace([held, other, again]).map(p => [p.place, p.reads.length])).toEqual([['user/QNTX', 2], ['user/ground', 1]]);
    });

    // Jenny: a control that names what it spoke about is counted under its own name.
    test('controls are counted by name', () => {
        const { body, scene } = drawn();
        scene.read([session('s-8', ['user/ground:main'], [
            turn('ground', 'unread-file-claim:a.go on Stop'),
            turn('ground', 'unread-file-claim:b.go on Stop'),
            turn('ground', 'no-comment-blocks on PreToolUse'),
        ])], () => {});
        const counted = [...body.querySelectorAll('.gr-surface .gr-counted span')].map(c => c.textContent);
        expect(counted).toEqual(['unread-file-claim2', 'no-comment-blocks1']);
    });

    // Jenny: only sessions a ritual walked were read, and that is said rather than an empty layer.
    test('no session a person held is said', () => {
        const { body, scene } = drawn();
        scene.read([walked], () => {});
        expect(body.querySelector('.gr-deep')?.textContent).toContain('a person started none of them');
    });
});
