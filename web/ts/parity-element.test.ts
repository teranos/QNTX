import { test, expect } from 'bun:test';
import { followedClades, renderHolds, renderSeam, short, type Held } from './parity-element';

// What parity hold gives of staands held to mcp, cut to three columns of Tool
// and one model nothing follows.
function held(): Held {
    return {
        signum: 'staands',
        sigil: '',
        reference: 'mcp',
        clades: [
            { model: 'Annotations', says: 'Optional annotations for the client.', says_from: '', score: 0,
              items: [{ column: 'audience', score: 0, followed: [], departs: [], says: 'Describes who the intended audience is.', says_from: '', required: false }] },
            { model: 'Tool', says: 'Definition for a tool the client can call.', says_from: '', score: 33,
              items: [
                  { column: 'description', score: 100, followed: ['protocol.Sigil.does'], departs: [],
                    says: 'A human-readable description of the tool.', says_from: '', required: false },
                  { column: 'inputSchema', score: 0, followed: ['protocol.Sigil.takes'],
                    departs: ['one value in the schema, and protocol.Sigil.takes is repeated'],
                    says: 'A JSON Schema object defining the expected parameters for the tool.', says_from: '', required: true },
                  { column: '_meta', score: 0, followed: [], departs: [], says: '', says_from: '', required: false },
              ] },
        ],
        unfollowed: { 'protocol.Signum': ['follows', 'sigils'] },
        missing: [],
        required: [],
        ours: {
            'protocol.Sigil': 'A Sigil is one thing the node does.',
            'protocol.Sigil.does': 'What it is for, in words, for somebody who has never seen the code.',
            'protocol.Sigil.takes': 'What goes in.',
            'protocol.Signum': 'A Signum holds the sigils of one subject.',
            'protocol.Signum.name': '',
        },
    };
}

test('a field of ours is shown without its package', () => {
    expect(short('protocol.Sigil.does')).toBe('Sigil.does');
});

// Tim: a pill per model anything follows, and the first one's seam is open.
test('the seam opens on the first model anything follows', () => {
    const content = document.createElement('div');
    renderHolds(content, [held()]);
    const pills = content.querySelectorAll('.parity-pill');
    expect(pills.length).toBe(1);
    expect(pills[0].textContent).toBe('mcp · Tool 33');
    expect(pills[0].getAttribute('aria-pressed')).toBe('true');
    expect(followedClades(held()).map(c => c.model)).toEqual(['Tool']);
});

// Both sides in their own words, row by row.
test('a column carries what the spec says of it, and its field what ours says', () => {
    const seam = document.createElement('div');
    const h = held();
    renderSeam(seam, h, h.clades[1]);
    const rows = [...seam.querySelectorAll('.parity-row')].map(row => row.textContent);
    expect(rows[0]).toContain('Definition for a tool the client can call.');
    expect(rows[0]).toContain('A Sigil is one thing the node does.');
    expect(rows[1]).toContain('A human-readable description of the tool.');
    expect(rows[1]).toContain('=');
    expect(rows[1]).toContain('What it is for, in words, for somebody who has never seen the code.');
    expect(rows[2]).toContain('REQUIRED');
    expect(rows[2]).toContain('≠');
    expect(rows[2]).toContain('one value in the schema, and protocol.Sigil.takes is repeated');
});

// Spike: what says nothing is said to say nothing, never left blank.
test('a column the spec says nothing of says so', () => {
    const seam = document.createElement('div');
    const h = held();
    renderSeam(seam, h, h.clades[1]);
    const meta = [...seam.querySelectorAll('.parity-row')][3];
    expect(meta.textContent).toContain('the spec says nothing of it');
    expect(meta.textContent).toContain('nothing follows');
    expect(seam.querySelector('.parity-out-of-spec')?.textContent).toContain('Signum: follows · sigils');
});

// A reference the node refused to hold says why, and the others still open.
test('a refused reference says why beside the ones held', () => {
    const content = document.createElement('div');
    renderHolds(content, [held()], ['umami: staands follows umami, and the schema has none of its columns']);
    expect(content.querySelector('.element-error')?.textContent).toContain('umami:');
    expect(content.querySelectorAll('.parity-pill').length).toBe(1);
});

// Tim: pressing a row copies it, both sides as read.
// "clicking a row should make it copy to clickboard"
test('a pressed row is copied, both sides and why it departs', () => {
    const seam = document.createElement('div');
    const h = held();
    const copied: string[] = [];
    renderSeam(seam, h, h.clades[1], (text) => copied.push(text));
    ([...seam.querySelectorAll('.parity-row')][2] as HTMLElement).click();
    expect(copied).toEqual([
        'inputSchema REQUIRED · A JSON Schema object defining the expected parameters for the tool.  ≠  Sigil.takes · What goes in.\n' +
        'one value in the schema, and protocol.Sigil.takes is repeated',
    ]);
});

// Words the schema does not have itself say where they were read.
test('words read from beside the schema say where', () => {
    const seam = document.createElement('div');
    const h = held();
    h.reference = 'umami';
    h.clades[1].items[0].says_from = 'openapi.json · WebsiteSession.screen';
    renderSeam(seam, h, h.clades[1]);
    const row = [...seam.querySelectorAll('.parity-row')][1];
    expect(row.textContent).toContain('from openapi.json · WebsiteSession.screen');
    expect([...seam.querySelectorAll('.parity-row')][2].textContent).not.toContain('from ');
});
