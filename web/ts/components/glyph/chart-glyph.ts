/**
 * Chart Glyph - Generic time-series visualization
 *
 * A reusable glyph for displaying time-series data with D3 charts.
 * Supports multiple chart types (area, line, bar) and flexible data sources.
 *
 * Use cases:
 * - API usage costs and request metrics
 * - System resource monitoring (CPU, memory, disk)
 * - Attestation creation trends
 * - Pulse job execution stats
 * - Custom time-series data from any source
 */

import * as d3 from 'd3';
import { log, SEG } from '../../logger';

/**
 * Time-series data point structure (flexible - maps to backend format)
 */
export interface TimeSeriesDataPoint {
    date: string; // ISO date string (YYYY-MM-DD)
    [key: string]: any; // Allow any fields from backend
}

/**
 * Chart configuration
 */
export interface ChartConfig {
    /** Field name for primary metric in API response (e.g., "cost", "cpu_percent") */
    primaryField: string;

    /** Field name for secondary metric in API response (e.g., "requests", "memory_mb") */
    secondaryField?: string;

    /** Primary metric label (e.g., "Cost", "CPU %") */
    primaryLabel: string;

    /** Secondary metric label (e.g., "Requests", "Memory MB") */
    secondaryLabel?: string;

    /** Primary metric color */
    primaryColor?: string;

    /** Secondary metric color */
    secondaryColor?: string;

    /** Chart type */
    chartType?: 'area' | 'line' | 'bar';

    /** Value formatter (e.g., d => `$${d.toFixed(2)}`) */
    formatValue?: (value: number) => string;

    /** Default time range */
    defaultRange?: 'week' | 'month';
}

/**
 * Chart glyph state manager
 */
export class ChartGlyphState {
    private element: HTMLElement | null = null;
    private data: TimeSeriesDataPoint[] | null = null;
    private config: Required<ChartConfig>;
    private currentRange: 'week' | 'month';

    constructor(
        private id: string,
        private dataSource: string | (() => Promise<TimeSeriesDataPoint[]>),
        config: ChartConfig
    ) {
        // Apply defaults
        this.config = {
            primaryField: config.primaryField,
            secondaryField: config.secondaryField || '',
            primaryLabel: config.primaryLabel,
            secondaryLabel: config.secondaryLabel || '',
            primaryColor: config.primaryColor || '#4ade80',
            secondaryColor: config.secondaryColor || '#60a5fa',
            chartType: config.chartType || 'area',
            formatValue: config.formatValue || ((v) => v.toFixed(2)),
            defaultRange: config.defaultRange || 'week',
        };
        this.currentRange = this.config.defaultRange;
    }

    /**
     * Update data from external source
     */
    updateData(data: TimeSeriesDataPoint[]): void {
        this.data = data;
        if (this.element) {
            this.render();
        }
    }

    /**
     * Create render function for glyph registration
     */
    createRenderer(): () => HTMLElement {
        return () => {
            const content = document.createElement('div');
            this.element = content;
            this.loadAndRender();
            return content;
        };
    }

    /**
     * Load data and render chart
     */
    private async loadAndRender(): Promise<void> {
        if (!this.element) return;

        // Show loading state
        this.element.innerHTML = `
            <div class="glyph-content" style="position: relative;">
                <div class="chart-container" id="chart-${this.id}">
                    <div class="glyph-loading">Loading chart data...</div>
                </div>
            </div>
        `;

        // Add view toggle control
        this.addViewToggle();

        try {
            // Fetch data
            if (typeof this.dataSource === 'function') {
                this.data = await this.dataSource();
            } else {
                const days = this.currentRange === 'week' ? 7 : 30;
                const { apiFetch } = await import('../../api');
                const response = await apiFetch(`${this.dataSource}?days=${days}`);

                if (!response.ok) {
                    throw new Error(`Failed to fetch: ${response.statusText}`);
                }

                this.data = await response.json() as TimeSeriesDataPoint[];
            }

            this.render();
        } catch (error) {
            log.warn(SEG.UI, `[ChartGlyph ${this.id}] Failed to load data:`, error);
            if (this.element) {
                const container = document.getElementById(`chart-${this.id}`);
                if (container) {
                    container.innerHTML = '<div class="glyph-loading">Failed to load chart data</div>';
                }
            }
        }
    }

    /**
     * Render D3 chart
     */
    private render(): void {
        const container = document.getElementById(`chart-${this.id}`);
        if (!container || !this.data || this.data.length === 0) {
            if (container) {
                container.innerHTML = '<div class="glyph-loading">No data available</div>';
            }
            return;
        }

        // Clear container
        container.innerHTML = '';

        const width = 500;
        const height = 300;
        const margin = { top: 30, right: 20, bottom: 40, left: 60 };
        const chartWidth = width - margin.left - margin.right;
        const chartHeight = height - margin.top - margin.bottom;

        // Create SVG
        const svg = d3.select(container)
            .append('svg')
            .attr('width', width)
            .attr('height', height);

        const g = svg.append('g')
            .attr('transform', `translate(${margin.left},${margin.top})`);

        // Parse dates and prepare data
        const parseTime = d3.timeParse('%Y-%m-%d');
        const chartData = this.data.map(d => ({
            date: parseTime(d.date)!,
            value: d[this.config.primaryField],
            secondary: this.config.secondaryField ? d[this.config.secondaryField] : undefined
        })).filter(d => d.date !== null);

        // Calculate total
        const total = this.data.reduce((sum, d) => sum + d[this.config.primaryField], 0);

        // Scales
        const x = d3.scaleTime()
            .domain(d3.extent(chartData, d => d.date) as [Date, Date])
            .range([0, chartWidth]);

        const y = d3.scaleLinear()
            .domain([0, (d3.max(chartData, d => d.value) || 0) * 1.1])
            .range([chartHeight, 0]);

        const y2 = this.config.secondaryLabel ? d3.scaleLinear()
            .domain([0, (d3.max(chartData, d => d.secondary || 0) || 0) * 1.1])
            .range([chartHeight, 0]) : null;

        // Draw primary metric
        if (this.config.chartType === 'area') {
            const area = d3.area<typeof chartData[0]>()
                .x(d => x(d.date))
                .y0(chartHeight)
                .y1(d => y(d.value))
                .curve(d3.curveMonotoneX);

            g.append('path')
                .datum(chartData)
                .attr('fill', this.config.primaryColor)
                .attr('fill-opacity', 0.3)
                .attr('d', area);
        } else {
            const line = d3.line<typeof chartData[0]>()
                .x(d => x(d.date))
                .y(d => y(d.value))
                .curve(d3.curveMonotoneX);

            g.append('path')
                .datum(chartData)
                .attr('fill', 'none')
                .attr('stroke', this.config.primaryColor)
                .attr('stroke-width', 2)
                .attr('d', line);
        }

        // Draw secondary metric if present
        if (this.config.secondaryLabel && y2) {
            const line = d3.line<typeof chartData[0]>()
                .x(d => x(d.date))
                .y(d => y2(d.secondary || 0))
                .curve(d3.curveMonotoneX);

            g.append('path')
                .datum(chartData)
                .attr('fill', 'none')
                .attr('stroke', this.config.secondaryColor)
                .attr('stroke-width', 2)
                .attr('d', line);
        }

        // X axis
        g.append('g')
            .attr('transform', `translate(0,${chartHeight})`)
            .call(d3.axisBottom(x)
                .ticks(this.currentRange === 'week' ? 7 : 10)
                .tickFormat(d3.timeFormat('%m/%d') as any))
            .selectAll('text')
            .style('fill', '#a0a0a0')
            .style('font-size', '11px');

        // Y axis
        g.append('g')
            .call(d3.axisLeft(y)
                .ticks(5)
                .tickFormat(d => this.config.formatValue(+d)))
            .selectAll('text')
            .style('fill', '#a0a0a0')
            .style('font-size', '11px');

        // Style axes
        g.selectAll('path, line')
            .style('stroke', '#505050');

        // Title
        g.append('text')
            .attr('x', chartWidth / 2)
            .attr('y', -10)
            .attr('text-anchor', 'middle')
            .style('fill', '#e8e8e8')
            .style('font-size', '14px')
            .style('font-weight', 'bold')
            .text(`${this.config.primaryLabel}: ${this.config.formatValue(total)} (${this.currentRange})`);

        // Legend
        const legend = g.append('g')
            .attr('transform', `translate(${chartWidth - 120}, 10)`);

        // Primary metric legend
        if (this.config.chartType === 'area') {
            legend.append('rect')
                .attr('width', 15)
                .attr('height', 15)
                .attr('fill', this.config.primaryColor)
                .attr('fill-opacity', 0.3);
        } else {
            legend.append('line')
                .attr('x1', 0)
                .attr('x2', 15)
                .attr('y1', 7)
                .attr('y2', 7)
                .attr('stroke', this.config.primaryColor)
                .attr('stroke-width', 2);
        }

        legend.append('text')
            .attr('x', 20)
            .attr('y', 12)
            .style('fill', '#a0a0a0')
            .style('font-size', '11px')
            .text(this.config.primaryLabel);

        // Secondary metric legend
        if (this.config.secondaryLabel) {
            legend.append('line')
                .attr('x1', 0)
                .attr('x2', 15)
                .attr('y1', 27)
                .attr('y2', 27)
                .attr('stroke', this.config.secondaryColor)
                .attr('stroke-width', 2);

            legend.append('text')
                .attr('x', 20)
                .attr('y', 32)
                .style('fill', '#a0a0a0')
                .style('font-size', '11px')
                .text(this.config.secondaryLabel);
        }

        // Add view toggle control after rendering
        this.addViewToggle();
    }

    /**
     * Add view toggle control (styled like window title bar controls)
     */
    private addViewToggle(): void {
        if (!this.element) return;

        const glyphContent = this.element.querySelector('.glyph-content');
        if (!glyphContent) return;

        // Remove existing toggle if present
        const existing = glyphContent.querySelector('.chart-view-toggle');
        if (existing) {
            existing.remove();
        }

        // Create toggle button styled like panel-minimize
        const toggle = document.createElement('button');
        toggle.textContent = this.currentRange === 'month' ? 'w' : 'm';
        toggle.title = this.currentRange === 'month' ? 'Switch to week view' : 'Switch to month view';
        toggle.className = 'chart-view-toggle';

        // Style to match window controls (.panel-minimize from window.css)
        toggle.style.position = 'absolute';
        toggle.style.top = '8px';
        toggle.style.right = '8px';
        toggle.style.background = 'transparent';
        toggle.style.border = '1px solid transparent';
        toggle.style.fontSize = '14px';
        toggle.style.color = 'rgba(255, 255, 255, 0.85)';
        toggle.style.cursor = 'pointer';
        toggle.style.padding = '0';
        toggle.style.width = '24px';
        toggle.style.height = '24px';
        toggle.style.display = 'flex';
        toggle.style.alignItems = 'center';
        toggle.style.justifyContent = 'center';
        toggle.style.borderRadius = '0';
        toggle.style.transition = 'background-color 0.15s ease, color 0.15s ease';
        toggle.style.fontFamily = 'monospace';
        toggle.style.fontWeight = 'bold';

        // Hover effect
        toggle.addEventListener('mouseenter', () => {
            toggle.style.background = 'var(--bg-hover)';
            toggle.style.color = 'rgba(255, 255, 255, 1)';
        });

        toggle.addEventListener('mouseleave', () => {
            toggle.style.background = 'transparent';
            toggle.style.color = 'rgba(255, 255, 255, 0.85)';
        });

        toggle.addEventListener('click', () => this.toggleRange());

        glyphContent.appendChild(toggle);
    }

    /**
     * Toggle between week and month views
     */
    private toggleRange(): void {
        this.currentRange = this.currentRange === 'week' ? 'month' : 'week';
        this.loadAndRender();
    }
}

/**
 * Create a chart glyph instance
 */
export function createChartGlyph(
    id: string,
    title: string,
    dataSource: string | (() => Promise<TimeSeriesDataPoint[]>),
    config: ChartConfig
): { id: string; title: string; renderContent: () => HTMLElement } {
    const state = new ChartGlyphState(id, dataSource, config);

    return {
        id,
        title,
        renderContent: state.createRenderer()
    };
}
