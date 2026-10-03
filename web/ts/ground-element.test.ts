/**
 * Ground element — one picture in cross-section, read from the nebula down.
 *
 * Personas: Tim (happy path), Spike (edge cases), Jenny (complex scenarios).
 */

import { describe, test, expect } from 'bun:test';
import { agoFor, byPlace, drawGround, openingOf, placeOf, upFor, walkOf, type Does, type Said, type Ug } from './ground-element';
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
    turn('agent', 'Start Explore'),
    turn('agent', 'Stop Explore'),
    turn('agent', 'Start Explore'),
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
    { name: 'up', note: '3d4h', symbol: '+' },
    { name: 'datapunt', note: '0.3.7', symbol: '+' },
];

const does: Does = {
    started: '2026-10-03T21:58:00Z',
    left: 3,
    watches: [
        { id: 'standing-ci-pushed', name: 'a push landed on a branch with CI', predicates: ['immediate:ci-status'] },
        { id: 'standing-dispatch-sent', name: 'a rite dispatched a workflow', predicates: ['immediate:dispatch'] },
    ],
    news: [
        { id: 'p-2:watching', name: 'ci', note: 'watching main 9f3c1aa 2 runs', symbol: '+', waiting: true, at: '2026-10-03T22:10:00Z', on_row: true },
        { id: 'p-1:162d82f', name: 'ci', note: 'failure main 162d82f 1/4', symbol: '!', waiting: false, at: '2026-10-03T22:05:00Z', on_row: false },
    ],
    failed: [],
};

function names(body: HTMLElement): string[] {
    return [...body.querySelectorAll('.gr-name b')].map(name => name.textContent ?? '');
}

function texts(body: HTMLElement, selector: string): string[] {
    return [...body.querySelectorAll(selector)].map(el => el.textContent ?? '');
}

function drawn(): { body: HTMLElement; scene: ReturnType<typeof drawGround> } {
    const body = document.createElement('div');
    return { body, scene: drawGround(body) };
}

describe('Ground - Tim', () => {
    // "THE ELEMENT NEEDS TO SHOW THESE IN ORDER"
    test('the picture is drawn in order, from the nebula down', () => {
        const { body } = drawn();
        expect(names(body)).toEqual(['Scry', 'Stars', 'Comet', 'Sky', 'Ground', 'Underground', 'Rituals and rites', 'Deeper', 'Errors']);
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
        const place = body.querySelector<HTMLElement>('.gr-deep .gr-place')!;
        expect(place.querySelector('.gr-place-name b')?.textContent).toBe('user/QNTX');
        expect(place.querySelector('.tr-opening')?.textContent).toBe('Build QNTX here');
        expect(texts(place, '.gr-under-line .gr-chip')).toEqual(['datapunt-owns-the-reference', 'main']);
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

    // Tim: the stars are what the node does for Ground, and nothing else the node says.
    test('what QNTX does for Ground hangs as stars', () => {
        const { body, scene } = drawn();
        scene.does(does);
        scene.said(row);
        expect(texts(body, '.gr-hung .gr-fact-name')).toEqual(['up', 'a push landed on a branch with CI', 'a rite dispatched a workflow', 'left on the row']);
        expect(texts(body, '.gr-hung .gr-fact-note').slice(1)).toEqual(['on immediate:ci-status', 'on immediate:dispatch', '3 conclusions']);
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
        scene.unsaid();
        expect(scry.classList.contains('gr-unreal')).toBe(true);
    });

    // "this part is supposed to show things QNTX does for ground specifically"
    test('what ci.watch said is written under the stars, a failure as unwell', () => {
        const { body, scene } = drawn();
        scene.does({ ...does, failed: [{ at: '2026-10-03T22:00:00Z', error: 'ci.watch: stopped waiting on main', execution_id: 'rearm:p-0' }] });
        const written = texts(body, '.gr-written .gr-fact-note');
        expect(written).toHaveLength(3);
        expect(written[0]).toContain('ci.watch: stopped waiting on main');
        expect(written[1]).toContain('watching main 9f3c1aa 2 runs');
        expect(texts(body, '.gr-written .gr-unwell .gr-fact-name')).toEqual(['ci.watch', 'ci']);
    });

    // Spike: a node that did not answer leaves no star of its last answer hanging.
    test('no star outlives its answer', () => {
        const { body, scene } = drawn();
        scene.does(does);
        scene.undone('/am/ground: HTTP 404');
        expect(body.querySelectorAll('.gr-said')).toHaveLength(0);
        expect(body.querySelector('.gr-stars')?.textContent).toContain('HTTP 404');
    });

    // Spike: the node's own words for how long: two units, no decimal.
    test('uptime is said as the status line says it', () => {
        const started = '2026-10-03T21:58:00Z';
        expect(upFor(started, Date.parse('2026-10-03T22:05:30Z'))).toBe('7m');
        expect(upFor(started, Date.parse('2026-10-04T01:03:00Z'))).toBe('3h5m');
        expect(upFor(started, Date.parse('2026-10-07T01:59:00Z'))).toBe('3d4h');
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

describe('Ground - underground', () => {
    const ago = (ms: number, keyLength: number) => new Date(Date.now() - ms).toISOString().slice(0, keyLength);
    const ug: Ug = {
        since: ago(86_400_000, 19) + 'Z',
        tmux: { asked: 240, last: ago(4000, 19) + 'Z', over: { [ago(120_000, 16)]: 12, [ago(60_000, 16)]: 11 } },
        sessions: [
            { session: 's-1-of-a-person', first: ago(7_200_000, 19) + 'Z', last: ago(600_000, 19) + 'Z', readings: 3, over: { [ago(7_200_000, 13)]: 1, [ago(3_600_000, 13)]: 2 } },
        ],
        windows: [
            { window: 'five_hour', readings: [{ at: ago(7_200_000, 19) + 'Z', used: 4 }, { at: ago(600_000, 19) + 'Z', used: 13 }] },
        ],
    };

    // "this seems like a QNTX change from the backend side, have we tackled this yet?"
    test('the underground is hatched until the node says what it sees of ug', () => {
        const { body, scene } = drawn();
        const under = body.querySelector<HTMLElement>('.gr-under')!;
        scene.does(does);
        expect(under.classList.contains('gr-unreal')).toBe(true);
        scene.does({ ...does, ug });
        expect(under.classList.contains('gr-unreal')).toBe(false);
        expect(under.querySelector('.gr-limit')?.textContent).toContain('which of them still run is not known here');
    });

    // "i expect you to also use sparklines underground"
    test('each ug is a line over time', () => {
        const { body, scene } = drawn();
        scene.does({ ...does, ug });
        expect(texts(body, '.gr-under .gr-line-name')).toEqual(['tmux ug', 's-1-of-a', 'five_hour']);
        expect(body.querySelectorAll('.gr-under .gr-line-spark svg.sparkline-line')).toHaveLength(3);
        const lasts = texts(body, '.gr-under .gr-line-last');
        expect(lasts[0]).toContain('s ago');
        expect(lasts[2]).toBe('13%');
        expect(body.querySelector('.gr-under')?.textContent).toContain('240 since QNTX began');
    });

    // Spike: a person with no tmux bar is told so, and gets no line for one.
    test('no tmux ug is said, not drawn', () => {
        const { body, scene } = drawn();
        scene.does({ ...does, ug: { ...ug, tmux: { asked: 0, last: '' } } });
        expect(texts(body, '.gr-under .gr-line-name')).toEqual(['s-1-of-a', 'five_hour']);
        expect(body.querySelector('.gr-under')?.textContent).toContain('No tmux ug of yours has asked');
    });

    // Jenny: a line told again is the row it already was, and a line of no change is not drawn again.
    test('a line told again is the same row', () => {
        const { body, scene } = drawn();
        scene.does({ ...does, ug });
        const before = [...body.querySelectorAll<HTMLElement>('.gr-under .gr-line')];
        const drawnBefore = before.map(row => row.querySelector('svg'));
        scene.does({ ...does, ug: { ...ug, tmux: { ...ug.tmux, asked: 241 } } });
        const after = [...body.querySelectorAll<HTMLElement>('.gr-under .gr-line')];
        after.forEach((row, i) => expect(row).toBe(before[i]));
        expect(after[1].querySelector('svg')).toBe(drawnBefore[1]);
    });

    // Spike: under a minute is said to the second, past it in the status line's words.
    test('how long ago', () => {
        const now = Date.parse('2026-10-03T22:00:00Z');
        expect(agoFor('2026-10-03T21:59:56Z', now)).toBe('4s ago');
        expect(agoFor('2026-10-03T21:53:00Z', now)).toBe('7m ago');
    });
});

describe('Ground - the core', () => {
    const said = 'API Error: Unable to connect to API (ENOTFOUND)\n\nRequest ID: req-1';
    const failing = session('s-9', ['grove', 'user/QNTX:main'], [
        turn('human', 'Performing ritual grove, rite 1 of 3: built. Nothing is asked of you'),
        turn('rite', 'built halt 2', 'ritual:rite:grove-2:built:9'),
        turn('error', `server_error: ${said}`, 'ground:payload:StopFailure:9'),
    ]);

    // "BUT ITS SACRED"
    test('what failed is said whole', () => {
        const { body, scene } = drawn();
        const chosen: string[] = [];
        scene.read([held, failing], id => chosen.push(id));
        scene.does({ ...does, failed: [{ at: '2026-10-03T22:00:00Z', error: 'ci.watch: stopped waiting on main@162d82f: context canceled', execution_id: 'rearm:p-0' }] });

        const titles = texts(body, '.gr-core .gr-err-title');
        expect(titles).toContain('server_error');
        expect(titles).toContain('rite built halt 2');
        expect(titles).toContain('ci.watch failed');
        expect(titles).toContain('ci failure main 162d82f 1/4');
        expect(texts(body, '.gr-core .gr-err-why')).toContain(said);
        expect(texts(body, '.gr-core .gr-err-why')).toContain('ci.watch: stopped waiting on main@162d82f: context canceled');

        const fatal = body.querySelector<HTMLElement>('.gr-core .gr-err-fatal')!;
        expect(fatal.querySelector('.gr-err-title')?.textContent).toBe('server_error');
        fatal.click();
        expect(chosen).toEqual(['s-9']);
    });

    // Spike: nothing failing is said, not left as an empty fire.
    test('nothing failed is said', () => {
        const { body, scene } = drawn();
        scene.read([held], () => {});
        scene.does({ ...does, news: [does.news[0]] });
        expect(body.querySelectorAll('.gr-core .gr-err')).toHaveLength(0);
        expect(body.querySelector('.gr-core')?.textContent).toContain('Nothing read here failed');
    });

    // Jenny: an error told again is the block it already was.
    test('what failed is not drawn again while nothing new fails', () => {
        const { body, scene } = drawn();
        scene.read([failing], () => {});
        const before = [...body.querySelectorAll('.gr-core .gr-err')];
        scene.does({ ...does, news: [does.news[0]] });
        const after = [...body.querySelectorAll('.gr-core .gr-err')];
        expect(after).toHaveLength(2);
        after.forEach((block, i) => expect(block).toBe(before[i]));
    });
});

describe('Ground - Jenny', () => {
    // Jenny: a thing told again is the star it already was, with its words changed in place.
    test('a star told again is the same star', () => {
        const { body, scene } = drawn();
        scene.does(does);
        const before = [...body.querySelectorAll<HTMLElement>('.gr-hung .gr-said')];
        scene.does({ ...does, left: 4, news: [does.news[1]] });
        const after = [...body.querySelectorAll<HTMLElement>('.gr-hung .gr-said')];
        expect(after).toHaveLength(4);
        after.forEach((star, i) => expect(star).toBe(before[i]));
        expect(after[3].querySelector('.gr-fact-note')?.textContent).toBe('4 conclusions');
        expect(body.querySelectorAll('.gr-written .gr-said')).toHaveLength(1);
    });

    // Jenny: more was left than is written, and how much more is said.
    test('what is not written is counted', () => {
        const { body, scene } = drawn();
        const many = Array.from({ length: 11 }, (_, i) => ({ ...does.news[1], id: `p-${i}` }));
        scene.does({ ...does, news: many });
        expect(body.querySelectorAll('.gr-written .gr-said')).toHaveLength(8);
        expect(body.querySelector('.gr-stars')?.textContent).toContain('and 3 earlier');
    });

    // "claude and other coding agents are in the sky"
    test('each agent is a cloud in the sky', () => {
        const { body, scene } = drawn();
        scene.read([held, walked], () => {});
        const ran = body.querySelector<HTMLElement>('.gr-sky .gr-agent-ran')!;
        expect(ran.querySelector('svg.gr-cloudlet')).not.toBeNull();
        expect(ran.querySelector('.gr-agent-name')?.textContent).toBe('claude-opus-5-5 · xhigh');
        expect(ran.querySelector('.gr-count')?.textContent).toBe('2 sessions');
        const sent = body.querySelector<HTMLElement>('.gr-sky .gr-agent-sent')!;
        expect(sent.querySelector('.gr-agent-name')?.textContent).toBe('Explore');
        expect(sent.querySelector('.gr-count')?.textContent).toBe('2 times');
    });

    // Jenny: a session a ritual walked is drawn by its rites, and is not among the sessions a person held.
    test('a walked session is a performance, one tile per rite', () => {
        const { body, scene } = drawn();
        scene.read([held, walked], () => {});
        expect(walkOf(walked)).toBe('grove');
        expect(walkOf(held)).toBeNull();
        const walk = body.querySelector<HTMLElement>('.gr-rites .gr-walk')!;
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
        expect(texts(body, '.gr-surface .gr-counted span')).toEqual(['unread-file-claim2', 'no-comment-blocks1']);
    });

    // Jenny: only sessions a ritual walked were read, and that is said rather than an empty layer.
    test('no session a person held is said', () => {
        const { body, scene } = drawn();
        scene.read([walked], () => {});
        expect(body.querySelector('.gr-deep')?.textContent).toContain('a person started none of them');
    });
});
