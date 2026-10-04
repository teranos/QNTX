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

// "this part is supposed to show things QNTX does for ground specifically"
// "claude and other coding agents are in the sky"

// Not drawn yet, a session's share of each embedding cluster: loom's ClusterBar.svelte at d512ffb2.

// Not drawn yet, every session file on disk and its import state: loom's SessionList.svelte at d512ffb2.

import type { Element } from '@teranos/elements';
import { tray, renderSparkline } from '@teranos/elements';
import { apiJson } from './client/http';
import { log, SEG } from './logger';
import { Ground } from './sym';
import { openTranscriptElement, when, type TranscriptRead, type Turn } from './components/element/transcript-element';
import { cloud, cloudBank, comet, core, horizon, nebula, seam, starburst, starField } from './ground-scene';
import { labelsOf, seenOver, seriesOf, type Window as Span } from './components/sparkline';
import { apiError, sacredEntry, type SacredError } from './components/sacred';

const ELEMENT_ID = 'ground-element';

// One thing the node says of itself on its status line (server/statusline_handlers.go StatusItem).
export interface Said {
    id?: string;
    name: string;
    note?: string;
    symbol: string;
}

// What the node does for Ground, as am ground answers it (server/am_ground.go).
export interface Does {
    started: string;
    left: number;
    watches: Array<{ id: string; name: string; predicates?: string[] }>;
    news: Array<{
        id: string; name: string; note: string; symbol: string; waiting: boolean; at: string; on_row: boolean;
        // What a conclusion carries in full: each workflow the push started, and how it ended.
        workflows?: Array<{ name: string; conclusion: string; url: string }>;
    }>;
    failed: Array<{ at: string; error: string; execution_id: string }>;
    // What the node sees of ug. A node from before it saw any answers without it.
    ug?: Ug;
}

// The tmux bar's asks by the minute, each session's readings by the hour, and
// what each window read: `over` is a count per bucket, keyed by the bucket's start in UTC.
export interface Ug {
    since: string;
    tmux: { asked: number; last: string; over?: Record<string, number> };
    sessions: Array<{ session: string; first: string; last: string; readings: number; over?: Record<string, number> }>;
    windows: Array<{ window: string; readings: Array<{ at: string; used: number }> }>;
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
        case 'error': return 'StopFailure';
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

// What the picture says of ug where the node says nothing of it.
const UG_UNSEEN = 'ug tmux asks /am/statusline and prints one line, and writes down the news it reads, for sky to carry. The status-line ug posts a usage reading for its session to QNTX. Nothing this element reads counts either, so how many ug there are is not known here.';

// What stays unknown once the node says what it sees of ug (ground ug/usage.d CLAIM_SQL).
const UG_SEEN = 'A status-line ug posts its session\'s first reading of a window, and after that only the ug that claims the interval posts, so which of them still run is not known here. Two tmux bars of one person are one asker to QNTX.';

// What Scry is, in its own README's words (qntx-plugins/scry/README.md).
const SCRY_IS = 'Scry is local inference through llama.cpp with Metal, a plugin of a node.';

// How far down its thread each star hangs: neighbours hang far apart, so what
// is written under one never reaches the next.
const DROPS = [14, 104, 28, 118];

// How long the node has been answering, in the words its own status line uses
// (server/statusline_handlers.go shortDuration): two units, no decimal.
export function upFor(started: string, now: number): string {
    const minutes = Math.max(0, Math.floor((now - Date.parse(started)) / 60000));
    const days = Math.floor(minutes / 1440);
    const hours = Math.floor(minutes / 60) % 24;
    if (days > 0) return `${days}d${hours}h`;
    if (hours > 0) return `${hours}h${minutes % 60}m`;
    return `${minutes}m`;
}

// How long ago, to the second under a minute and in the status line's words past it.
export function agoFor(at: string, now: number): string {
    const seconds = Math.max(0, Math.floor((now - Date.parse(at)) / 1000));
    return seconds < 60 ? `${seconds}s ago` : `${upFor(at, now)} ago`;
}

// How many of what ci.watch said are written under the stars; the count of the rest is said.
const WRITTEN_AT_MOST = 8;

// How many sessions' status-line ugs are drawn as lines; the count of the rest is said.
const SESSION_UGS_AT_MOST = 6;

// "i expect you to also use sparklines underground"

// One line underground: a name, its sparkline, and where it stands now.
interface Lined {
    key: string;
    name: string;
    spark: string;
    last: string;
}

interface Line {
    row: HTMLElement;
    spark: HTMLElement;
    last: HTMLElement;
    drawn: string;
}

// The lines of one block, each kept as the row it already was: its sparkline is
// drawn again only when its series changed, so pointing at one is not cut short.
function lineRows(into: HTMLElement, lines: Map<string, Line>, told: Lined[]): void {
    const kept = new Set<string>();
    told.forEach((one, i) => {
        kept.add(one.key);
        let line = lines.get(one.key);
        if (!line) {
            const row = make('div', 'gr-line');
            const spark = make('span', 'gr-line-spark');
            const last = make('span', 'gr-line-last');
            row.append(make('span', 'gr-line-name', one.name), spark, last);
            line = { row, spark, last, drawn: '' };
            lines.set(one.key, line);
        }
        if (line.drawn !== one.spark) {
            line.spark.innerHTML = one.spark;
            line.drawn = one.spark;
        }
        line.last.textContent = one.last;
        if (into.children[i] !== line.row) into.insertBefore(line.row, into.children[i] ?? null);
    });
    for (const [key, line] of lines) {
        if (kept.has(key)) continue;
        line.row.remove();
        lines.delete(key);
    }
}

// The tmux bar's asks, by the minute over the last hour.
function tmuxLined(ug: Ug, now: number): Lined[] {
    if (!ug.tmux.last) return [];
    const hour: Span = { start: now - 3_600_000, end: now, unit: 'minute' };
    const seen = seenOver('tmux ug', ug.tmux.over, ug.tmux.last);
    return [{
        key: 'tmux',
        name: 'tmux ug',
        spark: renderSparkline(seriesOf(seen.times, hour, seen.weights), labelsOf(hour), 'tmux ug, asks by the minute'),
        last: `asked ${agoFor(ug.tmux.last, now)}`,
    }];
}

// Each session whose status-line ug posted, by the hour over the last day.
function sessionLined(ug: Ug, now: number): Lined[] {
    const day: Span = { start: now - 86_400_000, end: now, unit: 'hour' };
    return ug.sessions.slice(0, SESSION_UGS_AT_MOST).map(drew => {
        const seen = seenOver(drew.session, drew.over, drew.last);
        return {
            key: drew.session,
            name: drew.session.substring(0, 8),
            spark: renderSparkline(seriesOf(seen.times, day, seen.weights), labelsOf(day), `ug of ${drew.session}, readings by the hour`),
            last: when(drew.last),
        };
    });
}

// What each window read, over the last day.
function windowLined(ug: Ug): Lined[] {
    return ug.windows.map(read => {
        const latest = read.readings[read.readings.length - 1];
        return {
            key: read.window,
            name: read.window,
            spark: renderSparkline(read.readings.map(r => r.used), read.readings.map(r => when(r.at)), `${read.window}, used`),
            last: latest ? `${latest.used}%` : '',
        };
    });
}

// One thing drawn as a star. Its key is where it came from, so the same thing
// told again is the star it already was.
interface Told {
    key: string;
    name: string;
    note: string;
    well: boolean;
}

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

// What hangs on threads, and what is written under them. A thing told again
// keeps its star, with its words changed in place; only a new thing is a new star.
function hang(hung: HTMLElement, written: HTMLElement, stars: Map<string, Star>, hanging: Told[], under: Told[]): void {
    const kept = new Set<string>();
    const place = (told: Told, into: HTMLElement): HTMLElement => {
        kept.add(told.key);
        let one = stars.get(told.key);
        if (!one) {
            one = star();
            stars.set(told.key, one);
        }
        one.name.textContent = told.name;
        one.note.textContent = told.note;
        one.node.classList.toggle('gr-unwell', !told.well);
        into.appendChild(one.node);
        return one.node;
    };
    hanging.forEach((told, i) => {
        const node = place(told, hung);
        node.style.left = `${((i + 0.5) / hanging.length) * 100}%`;
        node.style.width = `min(150px, calc(${200 / hanging.length}% - 30px))`;
        node.style.setProperty('--drop', `${DROPS[i % DROPS.length]}px`);
    });
    for (const told of under) place(told, written).removeAttribute('style');
    for (const [key, one] of stars) {
        if (kept.has(key)) continue;
        one.node.remove();
        stars.delete(key);
    }
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

// "AT THE BOTTOM OF THE GROUND ELEMENT IS FIERY RED ERROR RED FATAL RED HIGH ENTROPY MADNESS"
// "BUT ITS SACRED"

// One failure, as the Sacred Error it is drawn through, and the session it happened in when it happened in one.
interface Failed {
    error: SacredError;
    session: string;
}

// What failed inside the sessions read: an API error that ended a turn, said as
// the API said it, and a rite that halted, with the code it read.
function errorsIn(reads: TranscriptRead[]): Failed[] {
    const found: Failed[] = [];
    for (const read of reads) {
        const context = {
            surface: `session ${read.session.substring(0, 8)}`,
            region: [walkOf(read), placeOf(read).place].filter(Boolean).join(' · ') || undefined,
        };
        for (const turn of read.turns) {
            if (turn.speaker === 'error') {
                found.push({ session: read.session, error: apiError(turn.of, turn.at, turn.text, context) });
                continue;
            }
            const said = turn.text.split(' ');
            if (turn.speaker !== 'rite' || said[1] !== 'halt') continue;
            found.push({
                session: read.session,
                error: { id: turn.of, severity: 'error', context, title: `rite ${said[0]} halted`, why: said.length > 2 ? `code ${said[2]}` : '', at: turn.at },
            });
        }
    }
    return found;
}

// What failed on the node's side of Ground: ci.watch itself, and a push or a
// run it concluded unwell, with each workflow that did not succeed.
function errorsOf(done: Does): Failed[] {
    const failed = done.failed.map(failure => ({
        session: '',
        error: {
            id: `failed:${failure.execution_id}:${failure.at}`, severity: 'error' as const,
            context: { surface: 'QNTX', region: `ci.watch ${failure.execution_id}` },
            title: 'ci.watch failed', why: failure.error, at: failure.at,
        },
    }));
    const unwell = done.news.filter(news => !news.waiting && news.symbol !== '+').map(news => ({
        session: '',
        error: {
            id: `news:${news.id}`, severity: 'error' as const,
            context: { surface: 'QNTX', region: 'left on the status line' },
            title: `${news.name} ${news.note}`,
            why: (news.workflows ?? [])
                .filter(workflow => workflow.conclusion !== 'success' && workflow.conclusion !== 'skipped')
                .map(workflow => `${workflow.name}: ${workflow.conclusion} ${workflow.url}`).join('\n'),
            at: news.at,
        },
    }));
    return [...failed, ...unwell];
}

// One failure in the core: when it happened, the session to open when it has
// one, and the failure itself through the one render (components/sacred.ts).
function errorEntry(failed: Failed, onChoose: (session: string) => void): HTMLElement {
    const entry = sacredEntry(failed.error);
    const head = make('div', 'gr-err-when', when(failed.error.at));
    if (failed.session) {
        const open = make('button', 'gr-err-open', `open session ${failed.session.substring(0, 8)}`);
        open.addEventListener('click', () => { onChoose(failed.session); });
        head.appendChild(open);
    }
    entry.prepend(head);
    return entry;
}

// How wide an agent's cloud is drawn: wider the more it flew, by the root of the count.
function cloudWide(count: number): number {
    return Math.min(120, 52 + Math.round(Math.sqrt(count) * 14));
}

// One kind of agent as one cloud: what it is, and how often it flew.
function clouds(flew: Array<[string, number]>, kind: 'ran' | 'sent', counted: (count: number) => string): HTMLElement {
    const flight = make('div', 'gr-flight');
    flew.forEach(([name, count], i) => {
        const agent = make('div', `gr-agent gr-agent-${kind}`);
        agent.append(cloud(cloudWide(count), i + (kind === 'ran' ? 31 : 53)), make('span', 'gr-agent-name', name), make('span', 'gr-count', counted(count)));
        flight.appendChild(agent);
    });
    return flight;
}

// A session's model is what its SessionStart named and its effort what its last
// Stop ran at; an agent sent out is a SubagentStart, by its type (server/transcripts.go).
function sayAgents(into: HTMLElement, reads: TranscriptRead[]): void {
    const ran = tally(reads.map(read => [read.model || 'model not said', read.effort].filter(Boolean).join(' · ')));
    const sent = tally(reads.flatMap(read => read.turns
        .filter(turn => turn.speaker === 'agent' && turn.text.startsWith('Start'))
        .map(turn => turn.text.substring(5).trim() || 'agent not named')));
    into.replaceChildren();
    if (ran.length > 0) {
        into.append(
            make('div', 'gr-label', `Ran a session, in ${sessionsRead(reads.length)}:`),
            clouds(ran, 'ran', count => (count === 1 ? '1 session' : `${count} sessions`)),
        );
    }
    if (sent.length > 0) {
        into.append(
            make('div', 'gr-label', 'Sent out by them:'),
            clouds(sent, 'sent', count => (count === 1 ? 'once' : `${count.toLocaleString('en')} times`)),
        );
    }
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

/** What the picture is handed once it stands: what the node does for Ground, the row it says, and the sessions it read. */
export interface GroundScene {
    does(answer: Does): void;
    undone(why: string): void;
    said(items: Said[]): void;
    unsaid(): void;
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

    const stars = stratum('stars', 'Stars', 'QNTX: what it does for Ground', starField(420, 120, 7));
    const hung = make('div', 'gr-hung');
    const written = make('div', 'gr-written');
    // What no star says: that QNTX is being asked, that it waits on nothing, or why it did not answer.
    const unhung = make('div', 'tr-said');
    stars.body.prepend(hung);
    stars.body.append(written, unhung, limit('What QNTX left is kept in memory, the newest 256 of it for everybody: a restart empties it, and the count begins again.'));

    const fall = stratum('comet gr-unreal', 'Comet', 'the ground binary, built by QNTX, landing on earth', comet());
    fall.section.prepend(starField(150, 30, 11));
    fall.body.appendChild(limit('The comet does not exist yet. Ground is built where it runs, by make install, or fetched from GitHub Releases.'));

    const sky = stratum('sky', 'Sky', 'how we talk to QNTX and receive from it', cloudBank());
    const carried = make('div', 'gr-says');
    const flying = make('div', 'gr-says');
    sky.body.append(
        carried,
        flying,
        limit('Ground runs as a Claude Code hook, so the agents here are the ones Claude Code ran. A model is here only when its SessionStart named it: ug keeps each session\'s model and effort in Ground\'s own store, and that is not streamed.'),
        limit('One sky runs per tree, and Ground keeps each in its process table. Sky streams attestations, not that table, so how many skies there are is not known here.'),
        limit('What QNTX refused and what is still pending stay on the machine, as qntx_status and qntx_at. Sky tells Sentry both counts when it ends or warns, and tells QNTX neither. A row QNTX answered with a 4xx is parked there and not sent again.'),
    );

    const surface = stratum('surface', 'Ground', 'the ground itself', horizon());
    surface.section.insertBefore(make('div', 'gr-tiles'), surface.body);
    const runs = make('div', 'gr-says');
    surface.body.append(runs, limit('What each run cost stays on the machine: Ground writes it to its timing table and sky ships it to Sentry. PostToolUse reaches QNTX and no transcript reads it, so the whole count of runs is not here.'));

    // Real once the node says what it sees of ug (am ground's ug).
    const under = stratum('under gr-unreal', 'Underground', 'ug: how many of them, tmux ug', seam(3));
    const tmuxSaid = make('div', 'gr-label');
    const tmuxLines = make('div', 'gr-lines');
    const sessionSaid = make('div', 'gr-label');
    const sessionLines = make('div', 'gr-lines');
    const windowSaid = make('div', 'gr-label');
    const windowLines = make('div', 'gr-lines');
    const underLimit = limit(UG_UNSEEN);
    under.body.append(tmuxSaid, tmuxLines, sessionSaid, sessionLines, windowSaid, windowLines, underLimit);
    const tmuxKept = new Map<string, Line>();
    const sessionKept = new Map<string, Line>();
    const windowKept = new Map<string, Line>();

    const rites = stratum('rites', 'Rituals and rites', '', seam(5));
    const walks = make('div', 'gr-says');
    rites.body.append(walks, limit('How often a performance turned back, and who holds its mic, stay in Ground\'s ritual_position table: one holder for the whole performance, set again at every rite. The rites Ground runs before a session carries the performance name no session, and no transcript reads them.'));

    const deep = stratum('deep', 'Deeper', 'sessions, by place', seam(8));
    const sessions = make('div', 'gr-says');
    sessions.appendChild(make('div', 'tr-said', 'Reading the sessions Ground recorded here…'));
    deep.body.append(sessions, limit('Ground streams no project and no scope. A place here is the last two directories of the repo, or of where the session began, so a worktree is a place of its own.'));

    // The core, under everything: what failed, each one whole.
    const fire = stratum('core', 'Errors', 'sacred', seam(13));
    const sacred = make('div', 'gr-sacred');
    const unburnt = make('div', 'tr-said', 'Reading for what failed…');
    fire.body.append(
        sacred,
        unburnt,
        limit('Ground writes its own errors as immediate:exec-result rows, the same row an exec\'s result is, and nothing typed tells one from the other: no transcript reads them. Each also goes to Sentry.'),
        limit('A tool that failed reaches QNTX as PostToolUseFailure, and no transcript reads it. No row here names a severity, so each is drawn as an error.'),
    );
    fire.section.appendChild(core());

    body.replaceChildren(scry.section, stars.section, fall.section, sky.section, surface.section, under.section, rites.section, deep.section, fire.section);

    const lit = new Map<string, Star>();
    // What the node does for Ground, as it last answered: null is not answered yet, a string is why not.
    let done: Does | string | null = null;

    const restar = () => {
        const hanging: Told[] = [];
        const under: Told[] = [];
        const unsaid: string[] = [];
        if (done === null) {
            unsaid.push('Asking QNTX what it does for Ground…');
        } else if (typeof done === 'string') {
            unsaid.push(`QNTX did not say what it does for Ground: ${done}`);
        } else {
            if (done.started) hanging.push({ key: 'up', name: 'up', note: upFor(done.started, Date.now()), well: true });
            for (const watch of done.watches) {
                hanging.push({ key: `watch:${watch.id}`, name: watch.name, note: (watch.predicates ?? []).map(p => `on ${p}`).join(', '), well: true });
            }
            hanging.push({ key: 'left', name: 'left on the row', note: done.left === 1 ? '1 conclusion' : `${done.left.toLocaleString('en')} conclusions`, well: true });
            for (const failure of done.failed) {
                under.push({ key: `failed:${failure.execution_id}:${failure.at}`, name: 'ci.watch', note: `${failure.error} · ${when(failure.at)}`, well: false });
            }
            for (const news of done.news) {
                under.push({ key: `news:${news.id}`, name: news.name, note: `${news.note} · ${when(news.at)}`, well: news.symbol === '+' });
            }
            if (under.length === 0) unsaid.push('Nothing of yours is waited on now, and no conclusion is held.');
            if (under.length > WRITTEN_AT_MOST) unsaid.push(`and ${under.length - WRITTEN_AT_MOST} earlier`);
        }
        hang(hung, written, lit, hanging, under.slice(0, WRITTEN_AT_MOST));
        unhung.textContent = unsaid.join('\n');
        burrow(done !== null && typeof done !== 'string' ? done.ug : undefined);
        burn();
    };

    // What failed, in the sessions read and on the node's own side, newest
    // first. It is drawn again only when what failed changed.
    let failedInSessions: Failed[] | null = null;
    let choose: (session: string) => void = () => {};
    const burning = new Map<string, HTMLElement>();
    const burn = () => {
        if (failedInSessions === null && done === null) return;
        const onTheNode = done !== null && typeof done !== 'string' ? errorsOf(done) : [];
        const all = [...(failedInSessions ?? []), ...onTheNode].sort((a, b) => Date.parse(b.error.at) - Date.parse(a.error.at));
        unburnt.textContent = all.length === 0 ? 'Nothing read here failed: no API error, no halted rite, no failed run.' : '';
        // One failure is one node for as long as it is read: a new read moves it, and builds nothing again.
        const kept = new Set<string>();
        all.forEach((failed, i) => {
            kept.add(failed.error.id);
            let entry = burning.get(failed.error.id);
            if (!entry) {
                entry = errorEntry(failed, session => { choose(session); });
                burning.set(failed.error.id, entry);
            }
            if (sacred.children[i] !== entry) sacred.insertBefore(entry, sacred.children[i] ?? null);
        });
        for (const [id, entry] of burning) {
            if (kept.has(id)) continue;
            entry.remove();
            burning.delete(id);
        }
    };

    // The underground: what the node sees of ug, as lines over time.
    const burrow = (ug: Ug | undefined) => {
        const now = Date.now();
        under.section.classList.toggle('gr-unreal', !ug);
        relimit(underLimit, ug ? UG_SEEN : UG_UNSEEN);
        lineRows(tmuxLines, tmuxKept, ug ? tmuxLined(ug, now) : []);
        lineRows(sessionLines, sessionKept, ug ? sessionLined(ug, now) : []);
        lineRows(windowLines, windowKept, ug ? windowLined(ug) : []);
        tmuxSaid.textContent = '';
        sessionSaid.textContent = '';
        windowSaid.textContent = '';
        if (!ug) return;
        tmuxSaid.textContent = ug.tmux.last
            ? `tmux ug, asks by the minute over the last hour, ${ug.tmux.asked.toLocaleString('en')} since QNTX began:`
            : 'No tmux ug of yours has asked QNTX for its status line since it began.';
        const drew = ug.sessions.length;
        if (drew === 0) {
            sessionSaid.textContent = 'No status-line ug posted a reading in the last day.';
        } else {
            const more = drew > SESSION_UGS_AT_MOST ? `, the latest ${SESSION_UGS_AT_MOST} drawn` : '';
            sessionSaid.textContent = `Status-line ug, readings by the hour over the last day, for ${drew === 1 ? '1 session' : `${drew} sessions`}${more}:`;
        }
        if (ug.windows.length > 0) windowSaid.textContent = 'What ug read, over the last day:';
    };
    restar();

    return {
        does: (answer) => {
            done = answer;
            restar();
        },
        undone: (why) => {
            // What was answered before is not what the node says now: no star outlives its answer.
            done = why;
            restar();
        },
        said: (items) => {
            // The row names every plugin the node holds (server/statusline_handlers.go HandleStatusLine).
            const runs = items.find(item => item.name === 'scry');
            scry.section.classList.toggle('gr-unreal', !runs);
            inferring.textContent = runs ? `This node runs scry${runs.note ? ` ${runs.note}` : ''}.` : '';
            relimit(scryLimit, runs
                ? 'The nebula itself is drawn by scry\'s own element, where Metal is.'
                : `${SCRY_IS} This node's status line names no scry.`);
        },
        unsaid: () => {
            scry.section.classList.add('gr-unreal');
            inferring.textContent = '';
            relimit(scryLimit, SCRY_IS);
        },
        read: (reads, onChoose) => {
            saySky(carried, reads);
            sayAgents(flying, reads);
            sayRuns(runs, reads);
            failedInSessions = errorsIn(reads);
            choose = onChoose;
            burn();
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

// ci.watch says again what it waits on every few seconds (server/ci_pulse.go pickAdaptiveSleep).
const ASK_EVERY_MS = 5000;

// One Ground, so one follower of the node: a picture drawn again takes it over.
let following: ReturnType<typeof setInterval> | null = null;

function followNode(body: HTMLElement, scene: GroundScene): void {
    const ask = () => {
        apiJson<Does>('/am/ground')
            .then(answer => scene.does(answer))
            .catch((err: unknown) => {
                log.warn(SEG.UI, '[GroundElement] the node did not say what it does for Ground', err);
                scene.undone(err instanceof Error ? err.message : String(err));
            });
        apiJson<{ items?: Said[] }>('/am/statusline?format=json')
            .then(answer => scene.said(answer.items ?? []))
            .catch((err: unknown) => {
                log.warn(SEG.UI, '[GroundElement] the node did not say its status line', err);
                scene.unsaid();
            });
    };
    if (following !== null) clearInterval(following);
    ask();
    // A window put away keeps its picture, and asks again once it is looked at.
    following = setInterval(() => { if (body.isConnected && !document.hidden) ask(); }, ASK_EVERY_MS);
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
            followNode(body, scene);
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
