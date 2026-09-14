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

test('the rectangle is on the namespace being stood in', () => {
    expect(tilesHtml([ns('playground')], 'playground', false)).toContain('standing');
});

// The node had not answered yet, or would not. Drawing the rectangle anywhere
// at all here would be this row picking a namespace nobody said.
test('standing nowhere draws no rectangle', () => {
    expect(tilesHtml([ns('playground')], '', false)).not.toContain('standing');
});

// Where a person stands is one place, so pressing another namespace moves the
// rectangle rather than lighting a second one.
test('the rectangle is on one namespace and not the rest', () => {
    const html = tilesHtml([ns('system'), ns('pond'), ns('playground')], 'pond', false);
    expect(html.split('standing').length - 1).toBe(1);
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

test('the right-clicked button splits into three', () => {
    const html = tilesHtml([ns('pond')], '', false, { name: 'pond', sure: false });
    expect(html).toContain('data-part="back"');
    expect(html).toContain('data-part="toggle"');
    expect(html).toContain('data-part="end"');
});

test('the switch is a track with a knob, and the track carries the state', () => {
    const on = tilesHtml([ns('pond')], '', false, { name: 'pond', sure: false });
    expect(on).toContain('switch-track" data-on="true"');
    expect(on).toContain('switch-knob');
    const off: Namespace = { ...ns('pond'), definition: { owner: 'o', enabled: false, created_at: 't' } };
    expect(tilesHtml([off], '', false, { name: 'pond', sure: false })).toContain('switch-track" data-on="false"');
});

// The node refuses to delete an enabled namespace, so the X does nothing until
// the namespace is disabled.
test('the X is inert while enabled, active once disabled, sure after one press', () => {
    const off: Namespace = { ...ns('pond'), definition: { owner: 'o', enabled: false, created_at: 't' } };
    expect(tilesHtml([ns('pond')], '', false, { name: 'pond', sure: false })).toContain('data-end="inert"');
    expect(tilesHtml([off], '', false, { name: 'pond', sure: false })).toContain('data-end="active"');
    expect(tilesHtml([off], '', false, { name: 'pond', sure: true })).toContain('data-end="sure"');
});

// The store refuses to switch or delete a namespace no ns.toml defines, so the
// button must not offer either.
test('an undefined namespace has no switch and an inert X', () => {
    const html = tilesHtml([ns('ducks', false)], '', false, { name: 'ducks', sure: false });
    expect(html).not.toContain('switch-track');
    expect(html).toContain('data-state="undefined"');
    expect(html).toContain('data-end="inert"');
});

test('the state rides on the button', () => {
    const off: Namespace = { ...ns('pond'), definition: { owner: 'o', enabled: false, created_at: 't' } };
    expect(tilesHtml([ns('pond')], '', false)).toContain('data-state="enabled"');
    expect(tilesHtml([off], '', false)).toContain('data-state="disabled"');
    expect(tilesHtml([ns('ducks', false)], '', false)).toContain('data-state="undefined"');
});

// Default holds the nuke and nothing else: no switch and no X, since the node
// keeps it.
test('default right-clicked is the way back and the nuke', () => {
    const html = tilesHtml([ns('default')], 'system', false, { name: 'default', sure: false });
    expect(html).toContain('data-part="back"');
    expect(html).toContain('data-part="nuke" data-end="active"');
    expect(html).not.toContain('switch-track');
    expect(html).not.toContain('data-part="end"');
    expect(tilesHtml([ns('default')], 'system', false, { name: 'default', sure: true })).toContain('data-end="sure"');
});

test('only the right-clicked button is split', () => {
    const html = tilesHtml([ns('pond'), ns('lake')], '', false, { name: 'pond', sure: false });
    expect(html.split('data-part="back"').length - 1).toBe(1);
    expect(html).toContain('data-name="lake" title=');
});

test('system is leftmost, default directly after, projects as listed', () => {
    const html = tilesHtml([ns('pond'), ns('default'), ns('lake'), ns('system')], '', false);
    const at = (name: string) => html.indexOf(`data-name="${name}"`);
    expect(at('system')).toBeLessThan(at('default'));
    expect(at('default')).toBeLessThan(at('pond'));
    expect(at('pond')).toBeLessThan(at('lake'));
});

test('a name that is markup does not become markup', () => {
    const html = tilesHtml([ns('<script>x</script>')], '', false);
    expect(html).not.toContain('<script>');
});
