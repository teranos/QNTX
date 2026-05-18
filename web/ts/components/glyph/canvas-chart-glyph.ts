/**
 * Chart Glyph — attestation data visualization on canvas.
 *
 * Renders a D3 chart. Will meld above data-producing glyphs (ax, sigma)
 * and visualize whatever flows through the composition edge.
 *
 * Phase 1: standalone with dummy data, proves the glyph lives on canvas.
 */

import type { Glyph } from '@qntx/glyphs';
import { canvasPlaced, wireExpandToWindow, preventDrag } from '@qntx/glyphs';
import * as d3 from 'd3';
import { el } from '../../html-utils';

const CHART_SYMBOL = 'chart';

export { CHART_SYMBOL };

export async function createChartGlyph(glyph: Glyph): Promise<HTMLElement> {
    const { element } = canvasPlaced({
        glyph,
        className: 'canvas-chart-glyph',
        defaults: {
            x: glyph.x ?? 200,
            y: glyph.y ?? 200,
            width: glyph.width ?? 420,
            height: glyph.height ?? 280,
        },
        resizable: true,
        logLabel: 'ChartGlyph',
    });

    // Minimal fold bar (like note glyph) — buttons appear on hover
    const foldBar = el('div', {
        class: 'glyph-title-bar chart-fold-bar',
        style: {
            height: '20px', minHeight: '20px', padding: '0 4px',
            background: 'transparent', borderBottom: '1px solid rgba(100,116,139,0.2)',
            cursor: 'move', display: 'flex', alignItems: 'center',
            justifyContent: 'flex-end', gap: '2px',
        },
    });

    const expandBtn = el('button', { text: '\u2B06' });
    expandBtn.title = 'Expand to window';
    expandBtn.style.cssText = 'width:20px;height:18px;font-size:11px;padding:0;background:transparent;border:none;color:#64748b;cursor:pointer;opacity:0;transition:opacity 0.15s ease;display:flex;align-items:center;justify-content:center;';
    preventDrag(expandBtn);

    foldBar.addEventListener('mouseenter', () => { expandBtn.style.opacity = '1'; });
    foldBar.addEventListener('mouseleave', () => { expandBtn.style.opacity = '0'; });

    foldBar.appendChild(expandBtn);
    element.appendChild(foldBar);

    // Chart container — fills remaining space
    const chartContainer = el('div', {
        class: 'glyph-content-area',
        style: { flex: '1', overflow: 'hidden' },
    });
    element.appendChild(chartContainer);

    // Render chart
    renderChart(chartContainer);

    // Re-render on resize
    const observer = new ResizeObserver(() => {
        renderChart(chartContainer);
    });
    observer.observe(chartContainer);

    // Wire expand-to-window
    wireExpandToWindow({
        element,
        expandBtn,
        glyphId: glyph.id,
        title: 'Chart',
        symbol: CHART_SYMBOL,
        renderContent: () => {
            const content = el('div', { style: { flex: '1' } });
            renderChart(content);
            return content;
        },
        logLabel: 'ChartGlyph',
    });

    return element;
}

/** Render a D3 line chart with dummy data into container */
function renderChart(container: HTMLElement): void {
    container.innerHTML = '';

    const rect = container.getBoundingClientRect();
    const width = Math.max(rect.width, 120);
    const height = Math.max(rect.height, 80);
    const margin = { top: 8, right: 12, bottom: 24, left: 36 };
    const innerW = width - margin.left - margin.right;
    const innerH = height - margin.top - margin.bottom;

    // Dummy time-series data
    const now = Date.now();
    const day = 86400000;
    const data = Array.from({ length: 14 }, (_, i) => ({
        date: new Date(now - (13 - i) * day),
        value: Math.floor(Math.random() * 40 + 10 + i * 2),
    }));

    const svg = d3.select(container)
        .append('svg')
        .attr('width', width)
        .attr('height', height);

    const g = svg.append('g')
        .attr('transform', `translate(${margin.left},${margin.top})`);

    const x = d3.scaleTime()
        .domain(d3.extent(data, d => d.date) as [Date, Date])
        .range([0, innerW]);

    const y = d3.scaleLinear()
        .domain([0, (d3.max(data, d => d.value) || 0) * 1.1])
        .range([innerH, 0]);

    // Area
    const area = d3.area<typeof data[0]>()
        .x(d => x(d.date))
        .y0(innerH)
        .y1(d => y(d.value))
        .curve(d3.curveMonotoneX);

    g.append('path')
        .datum(data)
        .attr('fill', '#4ade80')
        .attr('fill-opacity', 0.15)
        .attr('d', area);

    // Line
    const line = d3.line<typeof data[0]>()
        .x(d => x(d.date))
        .y(d => y(d.value))
        .curve(d3.curveMonotoneX);

    g.append('path')
        .datum(data)
        .attr('fill', 'none')
        .attr('stroke', '#4ade80')
        .attr('stroke-width', 1.5)
        .attr('d', line);

    // X axis
    g.append('g')
        .attr('transform', `translate(0,${innerH})`)
        .call(d3.axisBottom(x).ticks(5).tickFormat(d3.timeFormat('%m/%d') as any))
        .selectAll('text')
        .style('fill', '#64748b')
        .style('font-size', '10px');

    // Y axis
    g.append('g')
        .call(d3.axisLeft(y).ticks(4))
        .selectAll('text')
        .style('fill', '#64748b')
        .style('font-size', '10px');

    // Style axis lines
    g.selectAll('path, line')
        .style('stroke', '#334155');
}
