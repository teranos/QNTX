/**
 * @jest-environment jsdom
 *
 * "For one changing value, direct view of time and value", "Same would go for
 * doughnut, revealing a legend as well", "And a longer hover should expand the
 * tooltip showing the bigger picture".
 */

import { describe, test, expect, beforeEach, afterEach } from 'bun:test';
import { TooltipManager } from './tooltip';
import { renderDoughnut, wholeLegend, CHART_COLORS } from './doughnut';
import { wholeLine } from './sparkline';

const USE_JSDOM = process.env.USE_JSDOM === '1';
const wait = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

describe('the tooltip', () => {
    if (!USE_JSDOM) {
        test.skip('Skipped locally (run with USE_JSDOM=1 to enable)', () => {});
        return;
    }

    let container: HTMLElement;
    let manager: TooltipManager;
    let detach: () => void;

    beforeEach(() => {
        document.body.innerHTML = '';
        container = document.createElement('div');
        document.body.appendChild(container);
        manager = new TooltipManager({ delay: 5, expandDelay: 40 });
        manager.expands('data-tooltip-series', wholeLine);
        manager.expands('data-tooltip-legend', wholeLegend);
        detach = manager.attach(container, '[data-tooltip-legend] [data-tooltip]');
    });

    afterEach(() => detach());

    const said = () => document.querySelector('.panel-tooltip')?.textContent ?? null;

    const line = (): HTMLElement => {
        const el = document.createElement('span');
        el.dataset.tooltipSeries = JSON.stringify([['2026-09-29 10', 1], ['2026-09-29 11', 0], ['2026-09-29 12', 4]]);
        el.getBoundingClientRect = () => ({ left: 0, width: 100, top: 0, bottom: 16, right: 100, height: 16, x: 0, y: 0, toJSON: () => ({}) });
        container.appendChild(el);
        return el;
    };

    const point = (el: Element, type: string, x: number) =>
        el.dispatchEvent(new MouseEvent(type, { clientX: x, bubbles: type !== 'mouseenter' }));

    test('pointing at a line says the moment under the pointer, and follows it', async () => {
        const el = line();
        point(el, 'mouseenter', 2);
        await wait(15);
        expect(said()).toBe('2026-09-29 10 · 1');
        point(el, 'mousemove', 98);
        expect(said()).toBe('2026-09-29 12 · 4');
    });

    test('a longer hover grows it into the whole line and every moment that saw something', async () => {
        const el = line();
        point(el, 'mouseenter', 50);
        await wait(60);
        const tip = document.querySelector('.panel-tooltip');
        expect(tip?.classList.contains('panel-tooltip-expanded')).toBe(true);
        expect(tip?.querySelector('svg polyline')).not.toBeNull();
        expect(tip?.textContent).toContain('2026-09-29 10 · 1');
        expect(tip?.textContent).toContain('2026-09-29 12 · 4');
        expect(tip?.textContent).not.toContain('2026-09-29 11 · 0');
    });

    test('pointing at a segment says its legend entry, a longer hover the whole legend', async () => {
        const ring = renderDoughnut([{ name: 'chatgpt.com', value: 480 }, { name: 'newsletter', value: 20 }]);
        container.appendChild(ring as unknown as Node);
        const segment = ring?.querySelector('.doughnut-segment') as Element;
        segment.dispatchEvent(new MouseEvent('mouseenter'));
        await wait(15);
        expect(said()).toBe('chatgpt.com · 480');
        await wait(45);
        expect(said()).toContain('chatgpt.com · 480');
        expect(said()).toContain('newsletter · 20');
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
