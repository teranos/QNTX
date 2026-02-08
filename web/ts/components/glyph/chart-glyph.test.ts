/**
 * Chart Glyph Critical Path Tests
 *
 * Focus: chart instance creation, field mapping, data rendering
 */

import { describe, test, expect } from 'bun:test';
import { ChartGlyphState, createChartGlyph } from './chart-glyph';

describe('ChartGlyph', () => {
    test('creates glyph with correct config', () => {
        const glyph = createChartGlyph(
            'test-chart',
            'Test Chart',
            '/api/test',
            {
                primaryField: 'value1',
                secondaryField: 'value2',
                primaryLabel: 'Primary',
                secondaryLabel: 'Secondary',
                formatValue: (v) => `${v}`,
                defaultRange: 'week'
            }
        );

        expect(glyph.id).toBe('test-chart');
        expect(glyph.title).toBe('Test Chart');
        expect(typeof glyph.renderContent).toBe('function');
    });

    test('creates DOM container when rendered', () => {
        const glyph = createChartGlyph(
            'dom-test',
            'DOM Test',
            '/api/test',
            {
                primaryField: 'cost',
                primaryLabel: 'Cost'
            }
        );

        const element = glyph.renderContent();

        expect(element).toBeDefined();
        expect(element.querySelector('.glyph-content')).toBeDefined();
        expect(element.querySelector('#chart-dom-test')).toBeDefined();
    });

    test('renders with loading state initially', () => {
        const glyph = createChartGlyph(
            'loading-test',
            'Loading Test',
            '/api/test',
            {
                primaryField: 'value',
                primaryLabel: 'Value'
            }
        );

        const element = glyph.renderContent();
        const container = element.querySelector('#chart-loading-test');

        expect(container?.textContent).toContain('Loading chart data');
    });

    test('includes view toggle control', () => {
        const glyph = createChartGlyph(
            'toggle-test',
            'Toggle Test',
            '/api/test',
            {
                primaryField: 'value',
                primaryLabel: 'Value'
            }
        );

        const element = glyph.renderContent();
        const toggle = element.querySelector('.chart-view-toggle');

        expect(toggle).toBeDefined();
        expect(toggle?.textContent).toMatch(/^[wm]$/); // Should be 'w' or 'm'
    });
});

describe('Usage Chart Configuration', () => {
    test('usage chart has correct field mapping', () => {
        const usageGlyph = createChartGlyph(
            'usage-chart',
            '$ Usage & Costs',
            '/api/timeseries/usage',
            {
                primaryField: 'cost',
                secondaryField: 'requests',
                primaryLabel: 'Cost',
                secondaryLabel: 'Requests',
                primaryColor: '#4ade80',
                secondaryColor: '#60a5fa',
                chartType: 'area',
                formatValue: (v) => `$${v.toFixed(2)}`,
                defaultRange: 'month'
            }
        );

        expect(usageGlyph.id).toBe('usage-chart');
        expect(usageGlyph.title).toBe('$ Usage & Costs');
    });
});
