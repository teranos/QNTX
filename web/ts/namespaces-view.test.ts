import { test, expect } from 'bun:test';
import { kindOf, tilesHtml, type Namespace } from './namespaces-view';

function ns(name: string, owner = true): Namespace {
    return {
        name,
        owner: owner ? { owner_did: 'did:key:znode', minted_by: 'me', created_at: '2026-08-17T09:00:00Z' } : null,
        kinds: ['attestations'],
    };
}

test('system and default are not projects', () => {
    expect(kindOf('system')).toBe('system');
    expect(kindOf('default')).toBe('default');
    expect(kindOf('playground')).toBe('project');
});

test('the kind rides on the tile, so the colour is not decided here', () => {
    const html = tilesHtml([ns('system'), ns('playground')], '', false);
    expect(html).toContain('data-kind="system"');
    expect(html).toContain('data-kind="project"');
});

test('selecting a project reveals the minus beside it', () => {
    const html = tilesHtml([ns('playground')], 'playground', false);
    expect(html).toContain('namespace-remove');
    expect(html).toContain('selected');
});

test('nothing selected means no minus anywhere', () => {
    expect(tilesHtml([ns('playground')], '', false)).not.toContain('namespace-remove');
});

// ADR-027: neither was created, so neither may be deleted — and an affordance
// that would be refused should not be drawn.
test('selecting system or default reveals no minus', () => {
    expect(tilesHtml([ns('system')], 'system', false)).not.toContain('namespace-remove');
    expect(tilesHtml([ns('default')], 'default', false)).not.toContain('namespace-remove');
});

test('the plus becomes the rectangle you type into', () => {
    expect(tilesHtml([], '', false)).toContain('namespace-add');
    const adding = tilesHtml([], '', true);
    expect(adding).not.toContain('namespace-add');
    expect(adding).toContain('namespace-new');
});

// A namespace nobody declared is real and lists, so the hover has to say that
// rather than leave the reader to guess at a blank.
test('an unowned namespace says nobody recorded it', () => {
    expect(tilesHtml([ns('ducks', false)], '', false)).toContain('nobody recorded who owns this');
});

test('a name that is markup does not become markup', () => {
    const html = tilesHtml([ns('<script>x</script>')], '', false);
    expect(html).not.toContain('<script>');
});
