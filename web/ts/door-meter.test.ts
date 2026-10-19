import { describe, expect, test } from 'bun:test';
import { meter } from './door-meter';

// The door says what its field did since it last said, and starts counting again.

function fake() {
    const said: { name: string; value: number; unit: string | undefined; attributes: Record<string, unknown> }[] = [];
    let t = 1000;
    const m = meter(
        (name, value, unit, attributes) => said.push({ name, value, unit, attributes }),
        () => t,
        () => ({ 'door.buffer': '786x1704' }),
    );
    return { said, m, advance: (ms: number) => { t += ms; } };
}

const valueOf = (said: { name: string; value: number; attributes: Record<string, unknown> }[], name: string, seen?: boolean) =>
    said.find(s => s.name === name && (seen === undefined || s.attributes['door.seen'] === seen))?.value;

describe('Tim', () => {
    test('the first frame drawn says the field started, with where it is', () => {
        const { said, m } = fake();
        m.called();
        expect(said).toEqual([]);
        m.drew(2, true);
        expect(said).toEqual([{ name: 'door.field.started', value: 1, unit: undefined, attributes: { 'door.buffer': '786x1704' } }]);
        m.drew(2, true);
        expect(said).toHaveLength(1);
    });

    test('a report says how long it lived, how often it was called, and what drawing cost', () => {
        const { said, m, advance } = fake();
        for (let i = 0; i < 4; i++) m.called();
        m.drew(3, true);
        m.drew(5, true);
        advance(2000);
        said.length = 0;
        m.report('hidden');
        expect(valueOf(said, 'door.field.alive')).toBe(2000);
        expect(valueOf(said, 'door.field.callbacks')).toBe(4);
        expect(valueOf(said, 'door.field.frames', true)).toBe(2);
        expect(valueOf(said, 'door.field.cpu', true)).toBe(8);
        expect(said.every(s => s.attributes['door.reason'] === 'hidden')).toBe(true);
        expect(said.some(s => s.name === 'door.field.stopped')).toBe(false);
    });
});

describe('Spike', () => {
    test('frames drawn while the canvas was not on the page are said apart', () => {
        const { said, m } = fake();
        m.drew(4, false);
        m.drew(1, true);
        said.length = 0;
        m.report('pagehide');
        expect(valueOf(said, 'door.field.frames', false)).toBe(1);
        expect(valueOf(said, 'door.field.cpu', false)).toBe(4);
        expect(valueOf(said, 'door.field.frames', true)).toBe(1);
    });

    test('a bucket with no frames is not said', () => {
        const { said, m } = fake();
        m.drew(1, true);
        said.length = 0;
        m.report('hidden');
        expect(said.some(s => s.name === 'door.field.frames' && s.attributes['door.seen'] === false)).toBe(false);
    });

    test('a report starts the count again, and a stop says it stopped', () => {
        const { said, m, advance } = fake();
        m.called();
        m.drew(2, true);
        advance(500);
        m.report('hidden');
        advance(300);
        said.length = 0;
        m.report('stop');
        expect(valueOf(said, 'door.field.alive')).toBe(300);
        expect(valueOf(said, 'door.field.callbacks')).toBe(0);
        expect(said.some(s => s.name === 'door.field.frames')).toBe(false);
        expect(said.some(s => s.name === 'door.field.stopped')).toBe(true);
    });
});
