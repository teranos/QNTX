/**
 * Chart Element Critical Path Tests
 *
 * Focus: chart instance creation, field mapping, data rendering
 */

import { describe, test, expect } from 'bun:test';
import { ChartElementState, createChartElement } from './chart-element';

describe('ChartElement', () => {
    test('creates element with correct config', () => {
        const item = createChartElement(
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

        expect(item.id).toBe('test-chart');
        expect(item.title).toBe('Test Chart');
        expect(typeof item.renderContent).toBe('function');
    });

    test('creates DOM container when rendered', () => {
        const item = createChartElement(
            'dom-test',
            'DOM Test',
            '/api/test',
            {
                primaryField: 'cost',
                primaryLabel: 'Cost'
            }
        );

        const element = item.renderContent();

        expect(element).toBeDefined();
        expect(element.querySelector('.element-content')).toBeDefined();
        expect(element.querySelector('#chart-dom-test')).toBeDefined();
    });

    test('renders with loading state initially', () => {
        const item = createChartElement(
            'loading-test',
            'Loading Test',
            '/api/test',
            {
                primaryField: 'value',
                primaryLabel: 'Value'
            }
        );

        const element = item.renderContent();
        const container = element.querySelector('#chart-loading-test');

        expect(container?.textContent).toContain('Loading chart data');
    });

    test('includes view toggle control', () => {
        const item = createChartElement(
            'toggle-test',
            'Toggle Test',
            '/api/test',
            {
                primaryField: 'value',
                primaryLabel: 'Value'
            }
        );

        const element = item.renderContent();
        const toggle = element.querySelector('.chart-view-toggle');

        expect(toggle).toBeDefined();
        expect(toggle?.textContent).toMatch(/^[wm]$/); // Should be 'w' or 'm'
    });
});

describe('Usage Chart Configuration', () => {
    test('usage chart has correct field mapping', () => {
        const usageElement = createChartElement(
            'usage-chart',
            'Usage & Costs',
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
            },
            '$'
        );

        expect(usageElement.id).toBe('usage-chart');
        expect(usageElement.title).toBe('Usage & Costs');
        expect(usageElement.symbol).toBe('$');
    });
});
