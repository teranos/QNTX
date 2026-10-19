import { describe, expect, test } from 'bun:test';
import { loops, meter } from './loop-meter';

// A loop says what it did since it last said, and starts counting again. The
// ones still standing say so when asked.

type Said = { name: string; value: number; unit: string | undefined; attributes: Record<string, unknown> };

function fake() {
    const said: Said[] = [];
    let t = 1000;
    const sink = (name: string, value: number, unit: string | undefined, attributes: Record<string, unknown>) => {
        said.push({ name, value, unit, attributes });
    };
    return { said, sink, now: () => t, advance: (ms: number) => { t += ms; } };
}

const valueOf = (said: Said[], name: string, seen?: boolean) =>
    said.find(s => s.name === name && (seen === undefined || s.attributes['loop.seen'] === seen))?.value;

describe('Tim', () => {
    test('the first run says the loop started, under its name and where it is', () => {
        const { said, sink, now } = fake();
        const m = meter('door.field', sink, now, () => ({ 'door.buffer': '786x1704' }));
        m.called();
        expect(said).toEqual([]);
        m.ran(2, true);
        expect(said).toEqual([{ name: 'loop.started', value: 1, unit: undefined, attributes: { 'door.buffer': '786x1704', 'loop.name': 'door.field' } }]);
        m.ran(2, true);
        expect(said).toHaveLength(1);
    });

    test('a report says how long it lived, how often it woke, and what its work cost', () => {
        const { said, sink, now, advance } = fake();
        const m = meter('door.field', sink, now, () => ({}));
        for (let i = 0; i < 4; i++) m.called();
        m.ran(3, true);
        m.ran(5, true);
        advance(2000);
        said.length = 0;
        m.report('hidden');
        expect(valueOf(said, 'loop.alive')).toBe(2000);
        expect(valueOf(said, 'loop.callbacks')).toBe(4);
        expect(valueOf(said, 'loop.runs', true)).toBe(2);
        expect(valueOf(said, 'loop.cpu', true)).toBe(8);
        expect(said.every(s => s.attributes['loop.reason'] === 'hidden' && s.attributes['loop.name'] === 'door.field')).toBe(true);
        expect(said.some(s => s.name === 'loop.stopped')).toBe(false);
    });
});

describe('Spike', () => {
    test('work whose drawing was not on the page is said apart', () => {
        const { said, sink, now } = fake();
        const m = meter('x', sink, now, () => ({}));
        m.ran(4, false);
        m.ran(1, true);
        said.length = 0;
        m.report('pagehide');
        expect(valueOf(said, 'loop.runs', false)).toBe(1);
        expect(valueOf(said, 'loop.cpu', false)).toBe(4);
        expect(valueOf(said, 'loop.runs', true)).toBe(1);
    });

    test('a loop that draws nothing says no seen at all', () => {
        const { said, sink, now } = fake();
        const m = meter('poll', sink, now, () => ({}));
        m.ran(1);
        said.length = 0;
        m.report('hidden');
        const runs = said.find(s => s.name === 'loop.runs');
        expect(runs?.value).toBe(1);
        expect('loop.seen' in (runs?.attributes ?? {})).toBe(false);
    });

    test('a report starts the count again, and a stop says it stopped', () => {
        const { said, sink, now, advance } = fake();
        const m = meter('x', sink, now, () => ({}));
        m.called();
        m.ran(2);
        advance(500);
        m.report('hidden');
        advance(300);
        said.length = 0;
        m.report('stop');
        expect(valueOf(said, 'loop.alive')).toBe(300);
        expect(valueOf(said, 'loop.callbacks')).toBe(0);
        expect(said.some(s => s.name === 'loop.runs')).toBe(false);
        expect(said.some(s => s.name === 'loop.stopped')).toBe(true);
    });
});

describe('Loops', () => {
    test('every loop still standing reports when asked, by name, and a stopped one does not', () => {
        const { said, sink, now, advance } = fake();
        const all = loops(sink, now);
        const door = all.start('door.field');
        const poll = all.start('brow.poll');
        all.start('ground.poll');
        advance(1000);
        door.report('stop');
        said.length = 0;
        all.reportAlive('hidden');
        const names = said.filter(s => s.name === 'loop.alive').map(s => s.attributes['loop.name']).sort();
        expect(names).toEqual(['brow.poll', 'ground.poll']);
        poll.report('stop');
        said.length = 0;
        all.reportAlive('hidden');
        expect(said.filter(s => s.name === 'loop.alive').map(s => s.attributes['loop.name'])).toEqual(['ground.poll']);
    });

    test('a loop that was reported while the page was hidden is still counted when it comes back', () => {
        const { said, sink, now, advance } = fake();
        const all = loops(sink, now);
        all.start('x');
        advance(100);
        all.reportAlive('hidden');
        advance(50);
        said.length = 0;
        all.reportAlive('pagehide');
        expect(valueOf(said, 'loop.alive')).toBe(50);
    });
});
