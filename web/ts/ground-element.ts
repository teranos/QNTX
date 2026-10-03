/**
 * Ground Element — aware of anything Ground.
 */

// "The ground element would be aware of anything ground"
// "Transcripts in Transcript / List of sessions in ground"
// "In the Tray, like Users, DB, Plugins"

// "Model and effort belong in Ground."

// Ground is drawn as one picture in cross-section, read from the stars down.

// "THE ELEMENT NEEDS TO SHOW THESE IN ORDER"
// "IN THE REAL UI IT WILL BE CROSSHATCHED OUT, EACH NOTHING THEIR LIMITATION WITH REGARDS TO WHAT GROUND OR SKY DOES"

// "NOT EVERYTHING ON THIS LIST IS PURE REALITY YET, BUT I WANT THIS TO BE REAL, AND I KNOW IT CAN BE."
// "The sessions live in the deeper layers, below rituals and rites."
// "Scry should be the strata at the very top the nebula"

// Not drawn yet, a session's share of each embedding cluster: loom's ClusterBar.svelte at d512ffb2.

// Not drawn yet, every session file on disk and its import state: loom's SessionList.svelte at d512ffb2.

import type { Element } from '@teranos/elements';
import { tray } from '@teranos/elements';
import { apiJson } from './client/http';
import { log, SEG } from './logger';
import { Ground } from './sym';
import { openTranscriptElement, when, type TranscriptRead, type Turn } from './components/element/transcript-element';
import { cloudBank, comet, horizon, nebula, seam, starburst, starField } from './ground-scene';

const ELEMENT_ID = 'ground-element';

// One thing the node says of itself on its status line (server/statusline_handlers.go StatusItem).
export interface Said {
    id?: string;
    name: string;
    note?: string;
    symbol: string;
}

// ─── What a session is, read off what the node answers ────────

// What a session opens with when no person typed it: Ground's briefing to the
// agent that walks a ritual (ground source/ritual/run.d), and what Claude Code
// hands a session by itself.
const BRIEFING = 'Performing ritual ';
const HANDED = ['<task-notification>', '<cross-session-message', '<agent-message'];

/** The ritual a session was started to walk, or null when a person started it. */
export function walkOf(read: TranscriptRead): string | null {
    const first = read.turns.find(t => t.speaker === 'human');
    if (!first || !first.text.startsWith(BRIEFING)) return null;
    const rest = first.text.substring(BRIEFING.length);
    let end = 0;
    while (end < rest.length && rest[end] !== ',' && rest[end] !== ' ') end++;
    const name = rest.substring(0, end);
    return name.endsWith('.') ? name.slice(0, -1) : name;
}

/** What a person first said in a session, past everything handed to it: one line, or nothing. */
export function openingOf(read: TranscriptRead): string {
    for (const turn of read.turns) {
        if (turn.speaker !== 'human') continue;
        const text = turn.text.trim();
        if (text.startsWith(BRIEFING) || HANDED.some(mark => text.startsWith(mark))) continue;
        for (const line of text.split('\n')) {
            const one = line.trim();
            // A paste is wrapped in a tag on a line of its own; what was pasted is what was said.
            if (one === '' || one.startsWith('<pasted_content') || one.startsWith('</pasted_content')) continue;
            return one;
        }
    }
    return '';
}

// Ground streams a subject as the last two directories of the repo, or of where
// the session began, then a colon and the branch (ground source/db.d
// attestEventAt). A rite's subject is its ritual, and holds no colon.
function whereOf(subject: string): { place: string; branch: string } | null {
    const colon = subject.indexOf(':');
    if (colon < 0) return null;
    return { place: subject.substring(0, colon), branch: subject.substring(colon + 1) };
}

/** A session's place, the first its subjects name, and the branches it touched there. */
export function placeOf(read: TranscriptRead): { place: string; branches: string[] } {
    let place = '';
    const branches: string[] = [];
    for (const subject of read.subjects) {
        const at = whereOf(subject);
        if (!at) continue;
        if (place === '') place = at.place;
        // Ground writes unknown where it read no branch: that is not a branch.
        if (at.place === place && at.branch !== 'unknown' && !branches.includes(at.branch)) branches.push(at.branch);
    }
    return { place, branches };
}

/** Sessions under their place, each place where its newest session falls. */
export function byPlace(reads: TranscriptRead[]): Array<{ place: string; reads: TranscriptRead[] }> {
    const places: Array<{ place: string; reads: TranscriptRead[] }> = [];
    for (const read of reads) {
        const { place } = placeOf(read);
        let held = places.find(p => p.place === place);
        if (!held) {
            held = { place, reads: [] };
            places.push(held);
        }
        held.reads.push(read);
    }
    return places;
}

// A rite's turn says its name, its verdict and its code (server/transcripts.go riteSaid).
interface Rite {
    verdict: string;
    said: string;
    of: string;
}

const VERDICTS = ['advance', 'hold', 'halt'];

function ritesOf(read: TranscriptRead): Rite[] {
    return read.turns.filter(t => t.speaker === 'rite').map(t => {
        const verdict = t.text.split(' ')[1] ?? '';
        return { verdict: VERDICTS.includes(verdict) ? verdict : 'other', said: t.text, of: t.of };
    });
}

// A rite's row is named ritual:rite, its performance, its rite and the second
// it ran (ground source/ritual/record.d attestRite).
const RITE_ROW = 'ritual:rite:';

function performanceOf(rite: Rite): string | null {
    if (!rite.of.startsWith(RITE_ROW)) return null;
    const parts = rite.of.substring(RITE_ROW.length).split(':');
    return parts.length < 3 ? null : parts.slice(0, parts.length - 2).join(':');
}

// Which hook a turn was read from (server/transcripts.go turnOf). What Ground's
// controls said and what a rite found are rows of their own, not a run of a hook.
function eventOf(turn: Turn): string | null {
    switch (turn.speaker) {
        case 'human': return 'UserPromptSubmit';
        case 'assistant': return 'Stop';
        case 'tool': case 'mcp': case 'edit': case 'read': case 'search': case 'write': return 'PreToolUse';
        case 'session': return turn.text.startsWith('End') ? 'SessionEnd' : 'SessionStart';
        case 'compaction': return 'PreCompact';
        case 'agent': return turn.text.startsWith('Stop') ? 'SubagentStop' : 'SubagentStart';
        case 'task': return 'TaskCompleted';
        default: return null;
    }
}

// How often each name was counted, the most counted first.
function tally(names: string[]): Array<[string, number]> {
    const counts = new Map<string, number>();
    for (const name of names) counts.set(name, (counts.get(name) ?? 0) + 1);
    return [...counts.entries()].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
}

// ─── Drawing ──────────────────────────────────────────────────

function make<K extends keyof HTMLElementTagNameMap>(tag: K, className: string, text = ''): HTMLElementTagNameMap[K] {
    const el = document.createElement(tag);
    if (className) el.className = className;
    if (text) el.textContent = text;
    return el;
}

// How many sessions the node answered: what every count in the picture is a count over.
function sessionsRead(count: number): string {
    return count === 1 ? 'the one session read here' : `the ${count} sessions read here`;
}

// What was counted, each name with how often: as chips, or run on as a line.
function counted(tallied: Array<[string, number]>, as: 'gr-chips' | 'gr-counted'): HTMLElement {
    const line = make('div', as);
    for (const [name, count] of tallied) {
        const one = make('span', as === 'gr-chips' ? 'gr-chip' : '', name);
        one.appendChild(make('b', '', count.toLocaleString('en')));
        line.appendChild(one);
    }
    return line;
}

// One layer of the picture: its plate, its name, and what it says.
function stratum(layer: string, name: string, is: string, plate: SVGSVGElement): { section: HTMLElement; body: HTMLElement } {
    const section = make('section', `gr-stratum gr-${layer}`);
    const body = make('div', 'gr-body');
    const head = make('header', 'gr-name');
    head.appendChild(make('b', '', name));
    if (is) head.appendChild(make('span', 'gr-is', is));
    body.appendChild(head);
    section.append(plate, body);
    return { section, body };
}

// What a layer cannot say yet, hatched, in what Ground or sky does instead.
function limit(text: string): HTMLElement {
    const hatched = make('p', 'gr-limit');
    hatched.appendChild(make('span', '', text));
    return hatched;
}

// A limitation that depends on what the node says is written again, on the tape it already has.
function relimit(hatched: HTMLElement, text: string): void {
    const tape = hatched.firstElementChild;
    if (tape) tape.textContent = text;
}

// What Scry is, in its own README's words (qntx-plugins/scry/README.md).
const SCRY_IS = 'Scry is local inference through llama.cpp with Metal, a plugin of a node.';

// How far down its thread each star hangs: neighbours hang far apart, so what
// is written under one never reaches the next.
const DROPS = [14, 104, 28, 118];

// What hangs is short, and few hang, so each keeps room for its words at the
// width of a phone. News, and what takes longer to say, is written under them.
const HANGS_UP_TO = 22;
const HUNG_AT_MOST = 6;

// One thing the node says, as one star for as long as the node says it.
interface Star {
    node: HTMLElement;
    name: HTMLElement;
    note: HTMLElement;
}

function star(): Star {
    const node = make('div', 'gr-said');
    const name = make('span', 'gr-fact-name');
    const note = make('span', 'gr-fact-note');
    node.append(starburst(22), name, note);
    return { node, name, note };
}

// The row as stars. A thing said again is the star it already was, with its
// words changed in place; only a thing newly said is a new star.
function hang(hung: HTMLElement, written: HTMLElement, stars: Map<string, Star>, items: Said[]): void {
    const said = new Set<string>();
    const hanging: Star[] = [];
    for (const item of items) {
        // Two things under one name on one row are two stars.
        let key = item.id ?? item.name;
        while (said.has(key)) key += ' again';
        said.add(key);
        let one = stars.get(key);
        if (!one) {
            one = star();
            stars.set(key, one);
        }
        const note = item.note ?? '';
        one.name.textContent = item.name;
        one.note.textContent = note;
        one.node.classList.toggle('gr-unwell', item.symbol !== '+');
        const hangs = !item.id && hanging.length < HUNG_AT_MOST && item.name.length <= HANGS_UP_TO && note.length <= HANGS_UP_TO;
        if (hangs) hanging.push(one);
        else one.node.removeAttribute('style');
        (hangs ? hung : written).appendChild(one.node);
    }
    for (const [key, one] of stars) {
        if (said.has(key)) continue;
        one.node.remove();
        stars.delete(key);
    }
    hanging.forEach((one, i) => {
        one.node.style.left = `${((i + 0.5) / hanging.length) * 100}%`;
        one.node.style.width = `min(92px, calc(${200 / hanging.length}% - 30px))`;
        one.node.style.setProperty('--drop', `${DROPS[i % DROPS.length]}px`);
    });
}

function sessionRow(read: TranscriptRead, onChoose: (session: string) => void): HTMLElement {
    const row = make('button', 'tr-session');
    const at = make('span', 'tr-when', read.started ? when(read.started) : '');
    const opening = make('span', 'tr-opening', openingOf(read) || read.session);
    const under = make('span', 'gr-under-line');
    for (const branch of placeOf(read).branches) under.appendChild(make('span', 'gr-chip', branch));
    const ran = make('span', 'tr-ran', [read.model, read.effort].filter(Boolean).join(' · '));
    under.append(ran, make('span', 'gr-count', read.turns.length === 1 ? '1 turn' : `${read.turns.length.toLocaleString('en')} turns`));
    row.append(at, opening, under);
    row.addEventListener('click', () => { onChoose(read.session); });
    return row;
}

// A session a ritual walked: the ritual, where, and one tile per rite it ran.
function walkRow(read: TranscriptRead, onChoose: (session: string) => void): HTMLElement {
    const row = make('button', 'tr-session gr-walk');
    const rites = ritesOf(read);
    const head = make('span', 'gr-walk-head');
    // A session no briefing started is named by the performance its first rite belongs to.
    const unbriefed = rites.length > 0 ? performanceOf(rites[0]) : null;
    head.append(
        make('b', '', walkOf(read) ?? unbriefed ?? read.session),
        make('span', 'tr-when', read.started ? when(read.started) : ''),
    );
    const { place, branches } = placeOf(read);
    if (place) head.appendChild(make('span', 'gr-chip', [place, ...branches].join(' · ')));
    row.appendChild(head);
    if (rites.length === 0) {
        head.appendChild(make('span', 'gr-count', 'no rite was read in this session'));
    } else {
        head.appendChild(make('span', 'gr-count', `reached ${rites[rites.length - 1].said}`));
        const band = make('span', 'gr-band');
        for (const rite of rites) {
            const tile = make('span', `gr-tile gr-verdict-${rite.verdict}`);
            tile.title = rite.said;
            band.appendChild(tile);
        }
        row.appendChild(band);
    }
    row.addEventListener('click', () => { onChoose(read.session); });
    return row;
}

/** Exported for tests: sessions under their place, each a button named by what a person first said in it. */
export function renderSessions(body: HTMLElement, reads: TranscriptRead[], onChoose: (session: string) => void): void {
    body.replaceChildren();
    if (reads.length === 0) {
        body.appendChild(make('div', 'tr-said', 'No session Ground recorded is in this namespace.'));
        return;
    }
    for (const held of byPlace(reads)) {
        const place = make('div', 'gr-place');
        const name = make('div', 'gr-place-name');
        name.append(
            make('b', '', held.place || 'no place named'),
            make('span', 'gr-is', held.reads.length === 1 ? '1 session' : `${held.reads.length} sessions`),
        );
        place.appendChild(name);
        for (const read of held.reads) place.appendChild(sessionRow(read, onChoose));
        body.appendChild(place);
    }
}

// Every turn is a row a hook wrote, and sky is what carries those to the node
// (ground source/stream.d).
function saySky(into: HTMLElement, reads: TranscriptRead[]): void {
    const rows = reads.reduce((sum, read) => sum + read.turns.length + read.folded, 0);
    into.replaceChildren();
    if (rows === 0) return;
    into.appendChild(make('div', 'gr-told', `${rows.toLocaleString('en')} rows came up through sky, in ${sessionsRead(reads.length)}.`));
}

// A control's turn says its name and the hook it spoke on (server/transcripts.go
// turnOf). Where it spoke about one thing, Ground names that after a colon
// (ground source/stop.d unread-file-claim, source/notification.d ritual-halt).
function controlOf(turn: Turn): string {
    const on = turn.text.lastIndexOf(' on ');
    const named = on < 0 ? turn.text : turn.text.substring(0, on);
    const colon = named.indexOf(':');
    return colon < 0 ? named : named.substring(0, colon);
}

function sayRuns(into: HTMLElement, reads: TranscriptRead[]): void {
    const turns = reads.flatMap(read => read.turns);
    const events = tally(turns.map(eventOf).filter((event): event is string => event !== null));
    const controls = tally(turns.filter(turn => turn.speaker === 'ground').map(controlOf));
    const folded = reads.reduce((sum, read) => sum + read.folded, 0);
    into.replaceChildren();
    if (events.length > 0) into.append(make('div', 'gr-label', `Hooks Ground ran on, in ${sessionsRead(reads.length)}:`), counted(events, 'gr-chips'));
    if (controls.length > 0) into.append(make('div', 'gr-label', 'Controls that spoke, and how often:'), counted(controls, 'gr-counted'));
    if (folded > 0) into.appendChild(make('div', 'gr-label', `${folded.toLocaleString('en')} more rows are folded into sigmas, and no turn is read back from one.`));
}

/** What the picture is handed once it stands: the row the node says, and the sessions it read. */
export interface GroundScene {
    said(items: Said[]): void;
    unsaid(why: string): void;
    read(reads: TranscriptRead[], onChoose: (session: string) => void): void;
    unread(why: string): void;
}

/** Exported for tests: the whole picture drawn into body, every stratum in order. */
export function drawGround(body: HTMLElement): GroundScene {
    // Real once the node's own row names a scry among its plugins.
    const scry = stratum('scry gr-unreal', 'Scry', 'the nebula', nebula());
    const inferring = make('div', 'gr-told');
    const scryLimit = limit(SCRY_IS);
    scry.body.append(inferring, scryLimit);

    const stars = stratum('stars', 'Stars', 'QNTX', starField(420, 120, 7));
    const hung = make('div', 'gr-hung');
    const written = make('div', 'gr-written');
    // What is said while no star hangs: that the row is being asked for, or why it was not said.
    const unhung = make('div', 'tr-said', 'Asking QNTX what it says of itself…');
    stars.body.prepend(hung);
    stars.body.append(written, unhung, limit('QNTX says one thing of itself at a time on its status line, its uptime among them, and holds each up to five minutes. It keeps no count of the news it left for Ground.'));

    const fall = stratum('comet gr-unreal', 'Comet', 'the ground binary, built by QNTX, landing on earth', comet());
    fall.section.prepend(starField(150, 30, 11));
    fall.body.appendChild(limit('The comet does not exist yet. Ground is built where it runs, by make install, or fetched from GitHub Releases.'));

    const sky = stratum('sky', 'Sky', 'how we talk to QNTX and receive from it', cloudBank());
    const carried = make('div', 'gr-says');
    sky.body.append(
        carried,
        limit('One sky runs per tree, and Ground keeps each in its process table. Sky streams attestations, not that table, so how many skies there are is not known here.'),
        limit('What QNTX refused and what is still pending stay on the machine, as qntx_status and qntx_at. Sky ships both counts to Sentry, not to QNTX.'),
    );

    const surface = stratum('surface', 'Ground', 'the ground itself', horizon());
    surface.section.insertBefore(make('div', 'gr-tiles'), surface.body);
    const runs = make('div', 'gr-says');
    surface.body.append(runs, limit('What each run cost stays on the machine: Ground writes it to its timing table and sky ships it to Sentry. PostToolUse reaches QNTX and no transcript reads it, so the whole count of runs is not here.'));

    const under = stratum('under gr-unreal', 'Underground', 'ug: how many of them, tmux ug', seam(3));
    under.body.appendChild(limit('ug tmux asks /am/statusline and prints one line, and ug writes down the news it reads, for sky to carry. Nothing this element reads counts them, so how many ug there are is not known here.'));

    const rites = stratum('rites', 'Rituals and rites', '', seam(5));
    const walks = make('div', 'gr-says');
    rites.body.append(walks, limit('How often a performance turned back, and who held the mic at each rite, stay in Ground\'s ritual_position table. The rites Ground runs before a session carries the performance name no session, and no transcript reads them.'));

    const deep = stratum('deep', 'Deeper', 'sessions, by place', seam(8));
    const sessions = make('div', 'gr-says');
    sessions.appendChild(make('div', 'tr-said', 'Reading the sessions Ground recorded here…'));
    deep.body.append(sessions, limit('Ground streams no project and no scope. A place here is the last two directories of the repo, or of where the session began, so a worktree is a place of its own.'));

    body.replaceChildren(scry.section, stars.section, fall.section, sky.section, surface.section, under.section, rites.section, deep.section);

    const hanging = new Map<string, Star>();
    let drawn = '';
    return {
        said: (items) => {
            // The row is asked for every two seconds and mostly says the same.
            const now = JSON.stringify(items);
            if (now === drawn) return;
            drawn = now;
            unhung.textContent = '';
            hang(hung, written, hanging, items);
            // The row names every plugin the node holds (server/statusline_handlers.go HandleStatusLine).
            const runs = items.find(item => item.name === 'scry');
            scry.section.classList.toggle('gr-unreal', !runs);
            inferring.textContent = runs ? `This node runs scry${runs.note ? ` ${runs.note}` : ''}.` : '';
            relimit(scryLimit, runs
                ? 'The nebula itself is drawn by scry\'s own element, where Metal is.'
                : `${SCRY_IS} This node's status line names no scry.`);
        },
        unsaid: (why) => {
            // What was said before is not what the node says now: no star outlives its row.
            drawn = '';
            hang(hung, written, hanging, []);
            unhung.textContent = `QNTX did not say its status line: ${why}`;
            scry.section.classList.add('gr-unreal');
            inferring.textContent = '';
            relimit(scryLimit, SCRY_IS);
        },
        read: (reads, onChoose) => {
            saySky(carried, reads);
            sayRuns(runs, reads);
            const walked = reads.filter(read => walkOf(read) !== null || ritesOf(read).length > 0);
            walks.replaceChildren(...walked.map(read => walkRow(read, onChoose)));
            if (walked.length === 0) walks.appendChild(make('div', 'tr-said', 'No session read here was walked by a ritual.'));
            const held = reads.filter(read => walkOf(read) === null);
            if (held.length === 0 && reads.length > 0) {
                sessions.replaceChildren(make('div', 'tr-said', `A ritual walked ${sessionsRead(reads.length)}; a person started none of them.`));
                return;
            }
            renderSessions(sessions, held, onChoose);
        },
        unread: (why) => {
            sessions.replaceChildren(make('div', 'tr-said', `the node did not list the sessions Ground recorded: ${why}`));
        },
    };
}

// ─── Element ──────────────────────────────────────────────────

// The node's own fastest frame lasts two seconds (server/statusline_carousel.go carouselFast).
const ROW_EVERY_MS = 2000;

// One Ground, so one follower of the row: a picture drawn again takes it over.
let following: ReturnType<typeof setInterval> | null = null;

function followRow(body: HTMLElement, scene: GroundScene): void {
    const ask = () => {
        apiJson<{ items?: Said[] }>('/am/statusline?format=json')
            .then(answer => scene.said(answer.items ?? []))
            .catch((err: unknown) => {
                log.warn(SEG.UI, '[GroundElement] the node did not say its status line', err);
                scene.unsaid(err instanceof Error ? err.message : String(err));
            });
    };
    if (following !== null) clearInterval(following);
    ask();
    // A window put away keeps its picture, and asks again once it is looked at.
    following = setInterval(() => { if (body.isConnected && !document.hidden) ask(); }, ROW_EVERY_MS);
}

export function createGroundElement(): Element {
    return {
        id: ELEMENT_ID,
        title: 'Ground',
        symbol: Ground,
        // The night the picture begins with: its dot, its window and its panel wear it.
        color: 'var(--night-void)',
        renderContent: () => {
            const body = document.createElement('div');
            body.className = 'ground';
            const scene = drawGround(body);
            followRow(body, scene);
            apiJson<{ transcripts: TranscriptRead[] }>('/api/transcripts')
                .then(answer => scene.read(answer.transcripts, openTranscriptElement))
                .catch((err: unknown) => {
                    log.error(SEG.UI, '[GroundElement] the node did not list the sessions Ground recorded', err);
                    scene.unread(err instanceof Error ? err.message : String(err));
                });
            return body;
        },
    };
}

/** Opens the Ground element. */
export function openGroundElement(): void {
    tray.open(ELEMENT_ID);
}
