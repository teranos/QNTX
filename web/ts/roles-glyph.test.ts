import { test, expect } from 'bun:test';
import { holdersText, lineFor, renderList, said, type Role } from './roles-glyph';

function worker(): Role {
    return {
        name: 'WORKER',
        write: ['visit:done'],
        read: ['visit:assigned', 'visit:done'],
        all: false,
        reach: ['/api/attestations'],
        granters: ['COORDINATOR'],
        holders: { garden: ['garden-worker', 'google:110169484474386276334'] },
    };
}

// A line is read out loud, and the cell shows it the way ADR-034 writes it.
test('a cell says the line the way the ADR writes it', () => {
    expect(said(worker(), 'WRITE')).toBe('visit:done');
    expect(said(worker(), 'READ')).toBe('visit:assigned visit:done');
    expect(said({ ...worker(), all: true }, 'READ')).toBe('visit:assigned visit:done by all');
    expect(said(worker(), 'REACH')).toBe('/api/attestations by COORDINATOR');
});

// What is typed is what is written: the words, and after `by` who.
test('a typed line becomes the attestation it is', () => {
    expect(lineFor('WRITE', 'WORKER', 'visit:done visit:started')).toEqual({
        subjects: ['WRITE'], predicates: ['visit:done', 'visit:started'], contexts: ['WORKER'],
    });
    expect(lineFor('READ', 'WORKER', 'visit:done by all')).toEqual({
        subjects: ['READ'], predicates: ['visit:done'], contexts: ['WORKER'], actors: ['all'],
    });
    expect(lineFor('REACH', 'WORKER', '/api/attestations by ROOT COORDINATOR')).toEqual({
        subjects: ['REACH'], predicates: ['/api/attestations'], contexts: ['WORKER'], actors: ['ROOT', 'COORDINATOR'],
    });
});

// No line is ever taken back by a word, so an empty line is not a write.
test('an empty line writes nothing', () => {
    expect(lineFor('WRITE', 'WORKER', '')).toBeNull();
    expect(lineFor('READ', 'WORKER', '  by all')).toBeNull();
});

test('the holders read per namespace', () => {
    expect(holdersText(worker())).toBe('garden: garden-worker, google:110169484474386276334');
    expect(holdersText({ ...worker(), holders: {} })).toBe('—');
});

// A role named at the + is a row before any line makes it, so the first line
// has somewhere to be typed.
test('a role being named is drawn as a row', () => {
    const container = document.createElement('div');
    renderList(container, [worker()], 'DATAPUNT');
    const rows = [...container.querySelectorAll('tr[data-role]')].map(r => (r as HTMLElement).dataset.role);
    expect(rows).toEqual(['WORKER', 'DATAPUNT']);
});
