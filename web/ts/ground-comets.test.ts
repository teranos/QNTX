/**
 * The Comet band of Ground: each repository a comet, the heaviest first.
 *
 * Personas: Tim (happy path), Spike (edge cases), Jenny (complex scenarios).
 */

import { describe, test, expect } from 'bun:test';
import { drawGround, type Does } from './ground-element';
import { COMETS_AT_MOST, comets, type Comet } from './ground-scene';

const does: Does = { started: '2026-10-09T21:58:00Z', left: 0, watches: [], news: [], failed: [] };

function shower(count: number): Comet[] {
    const list: Comet[] = [];
    for (let i = 0; i < count; i++) {
        list.push({ repo: `teranos/repo-${i}`, built: true, size: 2.1 - i * 0.1, moved: 74 - i * 7, state: i < 1 ? 'observing' : i < 5 ? 'observed' : 'unobserved' });
    }
    return list;
}

function drawn(onComet: (c: Comet) => void = () => {}): { body: HTMLElement; scene: ReturnType<typeof drawGround> } {
    const body = document.createElement('div');
    return { body, scene: drawGround(body, onComet) };
}

describe('Comets - Tim', () => {
    // "each repository is a comet"
    test('each repository the node names is a comet with a head to press', () => {
        const { body, scene } = drawn();
        scene.does({ ...does, comets: shower(3) });
        const band = body.querySelector('.gr-comet')!;
        expect(band.classList.contains('gr-unreal')).toBe(false);
        expect(band.classList.contains('gr-shower')).toBe(true);
        expect([...band.querySelectorAll('.gr-comet-hit')].map(hit => hit.getAttribute('aria-label'))).toEqual(['teranos/repo-0', 'teranos/repo-1', 'teranos/repo-2']);
        expect(band.querySelectorAll('.gr-comet-burst')).toHaveLength(3);
    });

    // "i can click on a comet"
    test('a head pressed is the comet chosen', () => {
        const chosen: Comet[] = [];
        const { body, scene } = drawn(c => chosen.push(c));
        scene.does({ ...does, comets: shower(2) });
        body.querySelectorAll<HTMLElement>('.gr-comet-hit')[1]!.click();
        expect(chosen.map(c => c.repo)).toEqual(['teranos/repo-1']);
    });

    // The name is over the head and nowhere else at rest.
    test('a head carries its name and size, for over it', () => {
        const { body, scene } = drawn();
        scene.does({ ...does, comets: [{ repo: 'teranos/ground', built: true, size: 1.6, moved: 60, state: 'observed' }, { repo: 'teranos/new', built: false, size: 0, moved: 3, state: 'unobserved' }] });
        expect([...body.querySelectorAll('.gr-comet-name')].map(name => name.textContent)).toEqual(['teranos/ground · 1.6 MB', 'teranos/new']);
    });
});

describe('Comets - Spike', () => {
    // A node from before comets answers without them: the band stays hatched.
    test('the band is hatched until the node names comets, and again when it stops', () => {
        const { body, scene } = drawn();
        const band = body.querySelector('.gr-comet')!;
        scene.does(does);
        expect(band.classList.contains('gr-unreal')).toBe(true);
        expect(band.querySelector('.gr-limit')?.textContent).toContain('names no comets');
        scene.does({ ...does, comets: shower(1) });
        expect(band.classList.contains('gr-unreal')).toBe(false);
        scene.undone('the node went away');
        expect(band.classList.contains('gr-unreal')).toBe(true);
        expect(band.querySelectorAll('.gr-comet-hit')).toHaveLength(0);
    });

    // Named and none: a real band with only stars in it, not a hatched one.
    test('no comet named is a clear sky, not a hatched one', () => {
        const { body, scene } = drawn();
        scene.does({ ...does, comets: [] });
        const band = body.querySelector('.gr-comet')!;
        expect(band.classList.contains('gr-unreal')).toBe(false);
        expect(band.querySelectorAll('.gr-comet-hit')).toHaveLength(0);
        expect(band.querySelectorAll('.gr-star').length).toBeGreaterThan(100);
    });

    // More than the band holds: the heaviest are drawn, the rest are not.
    test('the band holds ten, the heaviest first', () => {
        const { heads } = comets(shower(14));
        expect(heads).toHaveLength(COMETS_AT_MOST);
        // The heaviest is highest.
        expect(Math.min(...heads.map(h => h.y))).toBe(heads[0]!.y);
    });

    // "more active comets have longer trail"
    test('a comet that moved more carries more of a tail', () => {
        const { svg } = comets([
            { repo: 'a', built: true, size: 1, moved: 70, state: 'observed' },
            { repo: 'b', built: true, size: 1, moved: 5, state: 'observed' },
        ]);
        const grains = [...svg.querySelectorAll('.gr-comet-grain')].map(g => (g.getAttribute('d') as string).split('M').length);
        // Three grain paths per comet, the first comet's before the second's.
        const [a, b] = [grains.slice(0, 3).reduce((x, y) => x + y, 0), grains.slice(3, 6).reduce((x, y) => x + y, 0)];
        expect(a).toBeGreaterThan(b * 5);
        // The heavy one has hairlines under its tail, the light one a hair path with nothing on it.
        const hairs = [...svg.querySelectorAll('.gr-comet-hair')].map(h => h.getAttribute('d'));
        expect(hairs).toHaveLength(2);
        expect(hairs[0]).toContain('L');
        expect(hairs[1]).toBe('');
    });
});

describe('Comets - Jenny', () => {
    // The node says the same comets every few seconds: the band is not drawn again, so a head stays the same node.
    test('the same comets said again keep their heads', () => {
        const { body, scene } = drawn();
        const list = shower(2);
        scene.does({ ...does, comets: list });
        const before = body.querySelector('.gr-comet-hit');
        scene.does({ ...does, comets: shower(2) });
        expect(body.querySelector('.gr-comet-hit')).toBe(before);
        scene.does({ ...does, comets: [...list, { repo: 'teranos/third', built: false, size: 0, moved: 1, state: 'unobserved' }] });
        expect(body.querySelector('.gr-comet-hit')).not.toBe(before);
        expect(body.querySelectorAll('.gr-comet-hit')).toHaveLength(3);
    });
});
