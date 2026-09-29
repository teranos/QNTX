/**
 * @jest-environment jsdom
 *
 * The distillation chart: which predicates it draws, and what it says of each.
 */

import { describe, test, expect } from 'bun:test';
import { renderTimeseriesChart } from './db-element.ts';

describe('Distillation chart', () => {
    test('the most recently seen are drawn and named first, labelled with when, not how many', () => {
        const histograms: Record<string, Record<string, number>> = {
            often: { '2026-09-01 10': 900, '2026-09-02 10': 900 },
            lately: { '2026-09-20 10': 1 },
        };
        for (let i = 0; i < 10; i++) {
            histograms[`middling-${i}`] = { [`2026-09-1${i} 10`]: 5 };
        }
        const container = document.createElement('div');
        renderTimeseriesChart(container, histograms);

        const legend = Array.from(container.querySelectorAll('svg + div > span'))
            .map((item) => Array.from(item.querySelectorAll('span')).map((s) => s.textContent));
        expect(legend.length).toBe(10);
        expect(legend[0]).toEqual(['', 'lately', '2026-09-20 10']);
        expect(legend.map((l) => l[1])).not.toContain('often');
    });
});
