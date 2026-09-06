/**
 * The Self glyph draws the node. This is the section that draws the person
 * looking at it.
 */

import { test, expect } from 'bun:test';
import { personSection, type Person } from './self-person';

function tim(over: Partial<Person> = {}): Person {
    return {
        user: 'US-TIM',
        display_name: 'tim',
        name: 'tim',
        level: 'ROOT',
        namespaces: [],
        identity: 'https://mastodon.example/@tim',
        via: 'session',
        accounts: [{ provider: 'mastodon', canonical_id: 'https://mastodon.example/@tim', handle: '@tim@mastodon.example' }],
        keys: ['did:key:zBrowser'],
        ...over,
    };
}

// Tim opens the Self glyph and finds himself on it: what he is called, how he
// got in, and which of his accounts the node joined to him.
test('the person is drawn from what the node said', () => {
    const html = personSection(tim(), '');

    expect(html).toContain('tim');
    expect(html).toContain('ROOT');
    expect(html).toContain('session');
    expect(html).toContain('https://mastodon.example/@tim');
    expect(html).toContain('@tim@mastodon.example');
    expect(html).toContain('mastodon');
});

// ROOT walked up to no door, and no door is every namespace the node serves.
// A blank there would read as a person acting nowhere.
test('a User that came in by no door says so rather than showing a blank', () => {
    const html = personSection(tim(), '');

    expect(html).toContain('no door');
    expect(html).toContain('every namespace this node serves');
});

// A registration belongs to the door it arrived at, and that door is where it
// acts (ADR-032). Both are the person's, so both are on the glyph.
test('a public registration is drawn with its door and its namespace', () => {
    const html = personSection(tim({
        display_name: undefined,
        name: '',
        level: 'PUBLIC_REGISTRATION',
        door: 'garden',
        namespaces: ['garden'],
    }), '');

    expect(html).toContain('garden');
    expect(html).toContain('PUBLIC_REGISTRATION');
    expect(html).not.toContain('no door');
});

// A token speaks for whoever minted it, and the glyph says which it is rather
// than drawing a machine as a person at a keyboard.
test('a bearer token says it came in on a token', () => {
    const html = personSection(tim({ via: 'token', level: 'ATTESTOR', namespaces: ['pond'] }), '');

    expect(html).toContain('token');
    expect(html).toContain('pond');
});

// Spike is not signed in. The node's own words, never softened into a shrug,
// and nothing else drawn beside them.
test('nobody logged in is the refusal in the node\'s own words', () => {
    const html = personSection(null, 'this route is not yours');

    expect(html).toContain('this route is not yours');
    expect(html).not.toContain('glyph-label');
});

// Nothing asked yet is not nobody logged in.
test('before the node has been asked, nothing is drawn', () => {
    expect(personSection(null, '')).toBe('');
});

// A display name is a name a person chose, and it lands in a page.
test('a name that is markup does not become markup', () => {
    const html = personSection(tim({ name: '<script>x</script>' }), '');

    expect(html).not.toContain('<script>');
});
