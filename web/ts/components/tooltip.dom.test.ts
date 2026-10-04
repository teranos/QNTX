/**
 * @jest-environment jsdom
 *
 * A line and a ring say what is pointed at in tooltip form: an element of
 * @teranos/elements, a new one every time. "For one changing value, direct
 * view of time and value", "Same would go for doughnut, revealing a legend as
 * well", "And a longer hover should expand the tooltip showing the bigger picture".
 */

import { describe, test, expect, beforeEach } from 'bun:test';
import { renderDoughnut, CHART_COLORS } from './doughnut';
import { renderSparkline, wireLineTooltips } from '@teranos/elements';
import { SAID_TIMING } from './said';

const USE_JSDOM = process.env.USE_JSDOM === '1';
const wait = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

describe('said in tooltip form', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    beforeEach(() => {
        document.body.innerHTML = '';
        SAID_TIMING.delay = 5;
        SAID_TIMING.expandAfter = 30;
        SAID_TIMING.grace = 5;
    });

    const tooltips = () => Array.from(document.querySelectorAll<HTMLElement>('[data-form="tooltip"]'));
    const said = () => tooltips()[0]?.querySelector('.tooltip-title')?.textContent ?? null;
    const point = (el: Element, type: string, x: number) =>
        el.dispatchEvent(new MouseEvent(type, { clientX: x, bubbles: type !== 'pointerenter' }));

    const aLine = (): Element => {
        const holder = document.createElement('div');
        holder.innerHTML = renderSparkline([1, 0, 4], ['2026-09-29 10', '2026-09-29 11', '2026-09-29 12'], 'staand:page_view');
        document.body.appendChild(holder);
        const line = holder.querySelector('[data-tooltip-series]')!;
        line.getBoundingClientRect = () => ({ left: 0, width: 100, top: 0, bottom: 16, right: 100, height: 16, x: 0, y: 0, toJSON: () => ({}) }) as DOMRect;
        return line;
    };

    test('pointing at a line says the moment under the pointer, and follows it', async () => {
        wireLineTooltips(document, SAID_TIMING);
        const line = aLine();
        point(line, 'pointerover', 2);
        point(line, 'pointerenter', 2);
        await wait(15);
        expect(said()).toBe('2026-09-29 10 · 1');
        point(line, 'pointermove', 98);
        expect(said()).toBe('2026-09-29 12 · 4');
    });

    test('pointing at a segment says its entry, a longer hover the whole legend', async () => {
        const ring = renderDoughnut([{ name: 'chatgpt.com', value: 480 }, { name: 'newsletter', value: 20 }], 96, 'Source')!;
        document.body.appendChild(ring);
        const segment = ring.querySelector('.doughnut-segment')!;
        segment.dispatchEvent(new MouseEvent('pointerenter'));
        await wait(15);
        expect(said()).toBe('chatgpt.com · 480');
        await wait(40);
        const [tip] = tooltips();
        expect(tip!.textContent).toContain('chatgpt.com · 480');
        expect(tip!.textContent).toContain('newsletter · 20');
    });
});

describe('the ring', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    test('a segment per answer, in the order given and Umami\'s colors, and no count on it', () => {
        const ring = renderDoughnut([{ name: 'a', value: 3 }, { name: 'b', value: 1 }]);
        const segments = Array.from(ring?.querySelectorAll('.doughnut-segment') ?? []);
        expect(segments.map((s) => s.getAttribute('stroke'))).toEqual([CHART_COLORS[0], CHART_COLORS[1]]);
        expect(ring?.textContent).toBe('');
    });

    test('nothing said is no ring', () => {
        expect(renderDoughnut([])).toBeNull();
        expect(renderDoughnut([{ name: 'a', value: 0 }])).toBeNull();
    });
});
