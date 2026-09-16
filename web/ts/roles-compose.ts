/**
 * The composer: the next line, one slot at a time, picked from what exists.
 *
 * A line is one attestation, and writing one is filling its slots and
 * attesting. What each slot may hold is what the node already holds: the
 * names the lines mention, the words and paths they name, the tokens and
 * namespaces the node lists. Typing is for a name nothing has yet.
 */

import type { Line } from './roles-glyph';

/** The five kinds of line the gate reads, by what the line is about. */
export type Kind = 'REACH' | 'WRITE' | 'READ' | 'GRANT' | 'REVOKE';

export const KINDS: Kind[] = ['REACH', 'WRITE', 'READ', 'GRANT', 'REVOKE'];

/** The inverse of a REACH line and of a WRITE or READ line, the way
 *  role:revoked is the inverse of a grant: the marker beside the paths or
 *  the words. The latest line about a pair, a path or a word with a role,
 *  wins; a line about another pair is untouched. */
export const REACH_REVOKED = 'reach:revoked';
export const WORDS_REVOKED = 'words:revoked';
const MARKERS = new Set([REACH_REVOKED, WORDS_REVOKED, 'role:granted', 'role:revoked']);

/** What was picked or typed into the slots. */
export interface Slots {
    kind: Kind;
    // REACH: the paths. WRITE and READ: the words. GRANT and REVOKE: the role.
    what: string[];
    // REACH, WRITE and READ: the role. GRANT and REVOKE: the namespace.
    of: string;
    // GRANT and REVOKE: the name the role is handed to.
    who: string;
    // REACH: who may grant besides ROOT. READ: `all` for everyone's rows.
    by: string[];
    // REACH, WRITE and READ: the line takes its pairs away rather than
    // giving them.
    revoked?: boolean;
}

/** A token as the node lists it: its name, and where it acts. */
export interface TokenNamed {
    label: string;
    namespaces: string[];
}

/** A person as the node lists them: a route that reaches them, and the door
 *  they came in by, if any. */
export interface PersonNamed {
    route: string;
    door: string;
}

/** What the node holds, for the slots to offer. Nothing here is typed but a
 *  role no line names yet. */
export interface Known {
    roles: string[];
    words: string[];
    paths: string[];
    names: string[];
    namespaces: string[];
    // Where a who acts, by name: a token's namespaces from its record, a
    // person's door. A grant holds there, spelled as the record spells it.
    acts: Record<string, string[]>;
}

/** Everything the lines mention, plus what the node lists: tokens, people,
 *  namespaces, the predicates it holds, the paths it serves. */
export function knownFrom(
    lines: Line[],
    tokens: TokenNamed[],
    people: PersonNamed[],
    namespaces: string[],
    predicates: string[],
    served: string[],
): Known {
    const roles = new Set<string>();
    const words = new Set<string>(predicates);
    const paths = new Set<string>(served);
    const names = new Set<string>();
    const acts: Record<string, string[]> = {};
    for (const t of tokens) {
        names.add(t.label);
        acts[t.label] = t.namespaces;
    }
    for (const p of people) {
        names.add(p.route);
        acts[p.route] = p.door ? [p.door] : [];
    }
    for (const line of lines) {
        const subject = line.subjects[0]?.toUpperCase();
        // A marker is what a line does, not a path, a word or a role.
        const said = line.predicates.filter(p => !MARKERS.has(p));
        if (subject === 'REACH') {
            said.forEach(p => paths.add(p));
            line.contexts.forEach(c => roles.add(c.toUpperCase()));
        } else if (subject === 'WRITE' || subject === 'READ') {
            said.forEach(p => words.add(p));
            line.contexts.forEach(c => roles.add(c.toUpperCase()));
        } else {
            line.subjects.forEach(s => names.add(s));
            said.forEach(p => roles.add(p.toUpperCase()));
        }
    }
    const sorted = (s: Set<string>) => [...s].sort();
    return {
        roles: sorted(roles),
        words: sorted(words),
        paths: sorted(paths),
        names: sorted(names),
        namespaces: [...namespaces].sort(),
        acts,
    };
}

/** Where a grant to `who` may hold: what their record says, or every
 *  namespace when the record names none. */
export function holdsIn(k: Known, who: string): string[] {
    const named = k.acts[who];
    if (named && named.length > 0) return named;
    return k.namespaces;
}

/** The line read out loud, as it will be once attested. Empty slots read as
 *  their name in brackets so the shape is visible before it is filled. */
export function preview(s: Slots): string {
    const what = s.what.length ? s.what.join(' ') : '[…]';
    const by = s.by.length ? ` by ${s.by.join(' ')}` : '';
    switch (s.kind) {
        case 'REACH':
        case 'WRITE':
        case 'READ': {
            const marker = s.revoked ? `${s.kind === 'REACH' ? REACH_REVOKED : WORDS_REVOKED} ` : '';
            return `${s.kind} is ${marker}${what} of ${s.of || '[role]'}${by}`;
        }
        case 'GRANT':
            return `${s.who || '[who]'} is role:granted ${what} of ${s.of || '[namespace]'}`;
        case 'REVOKE':
            return `${s.who || '[who]'} is role:revoked ${what} of ${s.of || '[namespace]'}`;
    }
}

/** The attestation the slots are, on the wire, or the slot that is empty. */
export function compose(s: Slots): { line: Record<string, unknown> } | { missing: string } {
    const what = s.what.map(w => w.trim()).filter(w => w !== '');
    const of = s.of.trim();
    const who = s.who.trim();
    const by = s.by.map(b => b.trim()).filter(b => b !== '');
    const reachSaid = s.revoked ? [REACH_REVOKED, ...what] : what;
    const wordsSaid = s.revoked ? [WORDS_REVOKED, ...what] : what;
    switch (s.kind) {
        case 'REACH':
            if (what.length === 0) return { missing: 'a path' };
            if (of === '') return { missing: 'a role' };
            return { line: { subjects: ['REACH'], predicates: reachSaid, contexts: [of.toUpperCase()], actors: by } };
        case 'WRITE':
            if (what.length === 0) return { missing: 'a word' };
            if (of === '') return { missing: 'a role' };
            return { line: { subjects: ['WRITE'], predicates: wordsSaid, contexts: [of.toUpperCase()] } };
        case 'READ':
            if (what.length === 0) return { missing: 'a word' };
            if (of === '') return { missing: 'a role' };
            return { line: { subjects: ['READ'], predicates: wordsSaid, contexts: [of.toUpperCase()], actors: by } };
        case 'GRANT':
        case 'REVOKE':
            if (who === '') return { missing: 'who' };
            if (what.length === 0) return { missing: 'a role' };
            if (of === '') return { missing: 'a namespace' };
            return { line: {
                subjects: [who],
                predicates: [s.kind === 'GRANT' ? 'role:granted' : 'role:revoked', ...what.map(w => w.toUpperCase())],
                contexts: [of],
            } };
    }
}

/** Words typed with spaces between them. */
export function split(typed: string): string[] {
    return typed.split(' ').map(w => w.trim()).filter(w => w !== '');
}

/** One of the three lines a role is made of, offered beside a grant of it:
 *  on by default, with a way to opt out. */
export interface Implied {
    kind: 'WRITE' | 'READ' | 'REACH';
    on: boolean;
    what: string[];
    by: string[];
}

/** The lines a role has none of yet, so a grant does not hand somebody an
 *  empty name. REACH starts on the one path every role that writes needs;
 *  READ starts by all. A line the role already has is not offered. */
export function impliedFor(role: string, lines: Line[]): Implied[] {
    const name = role.trim().toUpperCase();
    if (name === '') return [];
    const has = (kind: string) => lines.some(l =>
        l.subjects[0]?.toUpperCase() === kind && l.contexts.some(c => c.toUpperCase() === name));
    const offered: Implied[] = [];
    if (!has('WRITE')) offered.push({ kind: 'WRITE', on: true, what: [], by: [] });
    if (!has('READ')) offered.push({ kind: 'READ', on: true, what: [], by: ['all'] });
    if (!has('REACH')) offered.push({ kind: 'REACH', on: true, what: ['/api/attestations'], by: [] });
    return offered;
}

/** Every attestation an Attest writes: the implied lines left on, then the
 *  line itself. The first slot found empty is named, and nothing is written. */
export function composeAll(s: Slots, implied: Implied[]): { lines: Array<Record<string, unknown>> } | { missing: string } {
    const own = compose(s);
    if ('missing' in own) return own;
    const lines: Array<Record<string, unknown>> = [];
    const role = s.what[0] ?? '';
    for (const each of implied) {
        if (!each.on) continue;
        const made = compose({ kind: each.kind, what: each.what, of: role, who: '', by: each.by });
        if ('missing' in made) return { missing: `${each.kind} ${made.missing}` };
        lines.push(made.line);
    }
    lines.push(own.line);
    return { lines };
}
