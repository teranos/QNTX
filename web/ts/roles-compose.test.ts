import { test, expect } from 'bun:test';
import { compose, composeAll, holdsIn, impliedFor, knownFrom, preview, split, type Slots } from './roles-compose';
import type { Line } from './roles-glyph';

function line(over: Partial<Line>): Line {
    return {
        id: 'AS-1', subjects: ['WRITE'], predicates: ['visit:done'], contexts: ['WORKER'],
        actors: ['google:1'], by: 'google:1', at: '2026-09-06T12:00:00Z', ...over,
    };
}

function slots(over: Partial<Slots>): Slots {
    return { kind: 'WRITE', what: [], of: '', who: '', by: [], ...over };
}

// "TYPING IS NEVER": what the slots offer is what the node holds. The names
// the lines mention, the tokens and people it lists, the predicates it
// holds, the paths it serves.
test('what exists is read off the lines and the node', () => {
    const known = knownFrom(
        [
            line({}),
            line({ subjects: ['REACH'], predicates: ['/api/attestations'], contexts: ['worker'] }),
            line({ subjects: ['tim'], predicates: ['role:granted', 'COORDINATOR'], contexts: ['garden'] }),
        ],
        [{ label: 'pond-sensor', namespaces: ['clean'] }],
        [{ route: 'google:1', door: 'TEST1' }, { route: 'apple:2', door: '' }],
        ['Clean', 'default'],
        ['datapunt:observed'],
        ['/api/namespaces'],
    );
    expect(known.roles).toEqual(['COORDINATOR', 'WORKER']);
    expect(known.words).toEqual(['datapunt:observed', 'visit:done']);
    expect(known.paths).toEqual(['/api/attestations', '/api/namespaces']);
    expect(known.names).toEqual(['apple:2', 'google:1', 'pond-sensor', 'tim']);
    expect(known.namespaces).toEqual(['Clean', 'default']);

    // "I SHOULD NOT HAVE TO TYPE THE NAMESPACE NAME": a grant holds where the
    // who's record says, spelled as the record spells it.
    expect(holdsIn(known, 'pond-sensor')).toEqual(['clean']);
    expect(holdsIn(known, 'google:1')).toEqual(['TEST1']);
    expect(holdsIn(known, 'apple:2')).toEqual(['Clean', 'default']);
    expect(holdsIn(known, 'tim')).toEqual(['Clean', 'default']);
});

// The line is read out loud as it fills, so the shape is seen before it is.
test('the preview reads the line as it will be', () => {
    expect(preview(slots({ kind: 'WRITE' }))).toBe('WRITE is […] of [role]');
    expect(preview(slots({ kind: 'READ', what: ['datapunt:observed'], of: 'DATAPUNT', by: ['all'] })))
        .toBe('READ is datapunt:observed of DATAPUNT by all');
    expect(preview(slots({ kind: 'GRANT', who: 'pond-sensor', what: ['DATAPUNT'], of: 'clean' })))
        .toBe('pond-sensor is role:granted DATAPUNT of clean');
});

// The four lines datapunt needs, as the composer writes them (ADR-034).
test('the slots become the attestation they are', () => {
    expect(compose(slots({ kind: 'WRITE', what: ['datapunt:observed'], of: 'datapunt' })))
        .toEqual({ line: { subjects: ['WRITE'], predicates: ['datapunt:observed'], contexts: ['DATAPUNT'] } });
    expect(compose(slots({ kind: 'READ', what: ['datapunt:observed'], of: 'DATAPUNT', by: ['all'] })))
        .toEqual({ line: { subjects: ['READ'], predicates: ['datapunt:observed'], contexts: ['DATAPUNT'], actors: ['all'] } });
    expect(compose(slots({ kind: 'REACH', what: ['/api/attestations'], of: 'DATAPUNT', by: [] })))
        .toEqual({ line: { subjects: ['REACH'], predicates: ['/api/attestations'], contexts: ['DATAPUNT'], actors: [] } });
    expect(compose(slots({ kind: 'GRANT', who: 'pond-sensor', what: ['datapunt'], of: 'clean' })))
        .toEqual({ line: { subjects: ['pond-sensor'], predicates: ['role:granted', 'DATAPUNT'], contexts: ['clean'] } });
    expect(compose(slots({ kind: 'REVOKE', who: 'pond-sensor', what: ['DATAPUNT'], of: 'clean' })))
        .toEqual({ line: { subjects: ['pond-sensor'], predicates: ['role:revoked', 'DATAPUNT'], contexts: ['clean'] } });
});

// An empty slot is named, not guessed: no line is written with a hole in it.
test('an empty slot is named', () => {
    expect(compose(slots({ kind: 'WRITE' }))).toEqual({ missing: 'a word' });
    expect(compose(slots({ kind: 'WRITE', what: ['x'] }))).toEqual({ missing: 'a role' });
    expect(compose(slots({ kind: 'GRANT', what: ['X'], of: 'clean' }))).toEqual({ missing: 'who' });
    expect(compose(slots({ kind: 'REACH', what: ['/a'] }))).toEqual({ missing: 'a role' });
});

// A grant of a name nothing has said anything about is a token holding an
// empty name, so the three lines the role lacks ride beside it, on by
// default. A line it already has is not offered.
test('a grant offers the lines the role lacks', () => {
    const fresh = impliedFor('datapunt', []);
    expect(fresh.map(i => i.kind)).toEqual(['WRITE', 'READ', 'REACH']);
    expect(fresh.every(i => i.on)).toBe(true);
    expect(fresh[1].by).toEqual(['all']);
    expect(fresh[2].what).toEqual(['/api/attestations']);

    const worker = impliedFor('WORKER', [line({}), line({ subjects: ['REACH'], predicates: ['/api/attestations'] })]);
    expect(worker.map(i => i.kind)).toEqual(['READ']);
    expect(impliedFor('', [])).toEqual([]);
});

// "create then creates 4 attestations in total", and one opted out is not
// written. An implied line with an empty slot stops the whole of it.
test('attest writes the implied lines left on, then the grant', () => {
    const s = slots({ kind: 'GRANT', who: 'pond-sensor', what: ['DATAPUNT'], of: 'clean' });
    const implied = impliedFor('DATAPUNT', []);
    implied[0].what = ['datapunt:observed'];
    implied[1].what = ['datapunt:observed'];
    const made = composeAll(s, implied);
    expect('lines' in made && made.lines.length).toBe(4);
    expect('lines' in made && made.lines[3]).toEqual({ subjects: ['pond-sensor'], predicates: ['role:granted', 'DATAPUNT'], contexts: ['clean'] });

    implied[2].on = false;
    expect('lines' in composeAll(s, implied) && composeAll(s, implied).lines.length).toBe(3);

    implied[1].what = [];
    expect(composeAll(s, implied)).toEqual({ missing: 'READ a word' });
});

test('words are split on spaces', () => {
    expect(split(' visit:done  visit:started ')).toEqual(['visit:done', 'visit:started']);
    expect(split('')).toEqual([]);
});
