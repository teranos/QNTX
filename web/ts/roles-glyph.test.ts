import { test, expect } from 'bun:test';
import { renderList, said, short, type Line } from './roles-glyph';

function line(over: Partial<Line>): Line {
    return {
        id: 'AS-1',
        subjects: ['WRITE'],
        predicates: ['visit:done'],
        contexts: ['WORKER'],
        actors: ['google:110169484474386276334'],
        by: 'google:110169484474386276334',
        at: '2026-09-06T12:00:42.817Z',
        ...over,
    };
}

// A line is read out loud: X is Y of Z, then what follows the writer after
// `by`. The writer has its own column and is not in the sentence.
test('a line reads as X is Y of Z by W', () => {
    expect(said(line({}))).toBe('WRITE is visit:done of WORKER');
    expect(said(line({ subjects: ['READ'], actors: ['google:1', 'all'] }))).toBe('READ is visit:done of WORKER by all');
    expect(said(line({ subjects: ['REACH'], predicates: ['/api/attestations'], actors: ['google:1', 'COORDINATOR'] })))
        .toBe('REACH is /api/attestations of WORKER by COORDINATOR');
    expect(said(line({ subjects: ['tim'], predicates: ['role:granted', 'WORKER'], contexts: ['default'] })))
        .toBe('tim is role:granted WORKER of default');
});

test('a DID in a line is its last eight', () => {
    expect(short('did:key:z6MkuibmftN7apH2C7NR1iduBvndKR7J46C5kPLxCSCn1XDK')).toBe('CSCn1XDK');
    expect(said(line({ subjects: ['did:key:z6MkuibmftN7apH2C7NR1iduBvndKR7J46C5kPLxCSCn1XDK'], predicates: ['role:granted', 'WORKER'], contexts: ['Clean'] })))
        .toBe('CSCn1XDK is role:granted WORKER of Clean');
});

// One row per line, in the order the node answered, and nothing folded: a
// grant and the revoke that answers it are both rows.
test('every line is a row, superseded ones included', () => {
    const container = document.createElement('div');
    renderList(container, [
        line({ id: 'AS-2', subjects: ['spike'], predicates: ['role:revoked', 'WORKER'], contexts: ['default'], at: '2026-09-07T10:00:00Z' }),
        line({ id: 'AS-1', subjects: ['spike'], predicates: ['role:granted', 'WORKER'], contexts: ['default'] }),
    ]);
    const rows = [...container.querySelectorAll('tr[data-id]')].map(r => r.querySelector('td')?.textContent);
    expect(rows).toEqual(['spike is role:revoked WORKER of default', 'spike is role:granted WORKER of default']);
});
