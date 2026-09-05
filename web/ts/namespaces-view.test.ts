import { test, expect } from 'bun:test';
import { kindOf, tilesHtml, type Namespace } from './namespaces-view';

function ns(name: string, defined = true): Namespace {
    return {
        name,
        definition: defined ? { owner: 'google:104729', enabled: true, created_at: '2026-08-17T09:00:00Z' } : null,
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

test('selecting a namespace marks it', () => {
    expect(tilesHtml([ns('playground')], 'playground', false)).toContain('selected');
});

test('nothing selected marks nothing', () => {
    expect(tilesHtml([ns('playground')], '', false)).not.toContain('selected');
});

test('the plus becomes the rectangle you type into', () => {
    expect(tilesHtml([], '', false)).toContain('namespace-add');
    const adding = tilesHtml([], '', true);
    expect(adding).not.toContain('namespace-add');
    expect(adding).toContain('namespace-new');
});

// A namespace nobody defined is real and lists, so the hover has to say that
// rather than leave the reader to guess at a blank.
test('a namespace with no ns.toml says so', () => {
    expect(tilesHtml([ns('ducks', false)], '', false)).toContain('no ns.toml defines this');
});

test('the hover says whether the namespace is enabled', () => {
    expect(tilesHtml([ns('pond')], '', false)).toContain('enabled, owned by google:104729');
});

test('a name that is markup does not become markup', () => {
    const html = tilesHtml([ns('<script>x</script>')], '', false);
    expect(html).not.toContain('<script>');
});
