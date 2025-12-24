// Usage badge component for real-time cost tracking
//
// TODO(future): Multi-dimensional time-series patterns
// Research patterns for per-model cost breakdown in charts. Consider:
// - Stacked area charts (cost composition by model)
// - Interactive legend with model filtering
// - Trade-offs: visual complexity vs. insight value
// - Inspiration: Grafana multi-series dashboards, Datadog cost analytics
//
// TODO(future): Budget alerting with notifications
// Implement cost threshold monitoring with user notifications:
// - Config: User-defined budget limits (daily/weekly/monthly)
// - Detection: Check total cost vs. budget in renderChart()
// - Notification: Toast alert when threshold crossed
// - Persistence: Store alert state to avoid repeat notifications
// - UX: Clear visual indication of budget status
//
// TODO(future): Period-over-period comparison
// Add percentage change indicators for cost trends:
// - Fetch previous period data (e.g., week N vs. week N-1)
// - Calculate: ((current - previous) / previous) * 100
// - Display: Small indicator next to badge (e.g., "+15% ↑" or "-8% ↓")
// - Color coding: Green for decrease, red for increase
// - Consider: Week-over-week, month-over-month, year-over-year

import * as d3 from 'd3';

// Type definitions for usage data
interface UsageStats {
    total_cost: number;
    total_requests?: number;
    models?: Record<string, { cost: number; requests: number }>;
}

interface TimeSeriesDataPoint {
    date: string;
    cost: number;
    requests: number;
}

// State
let currentStats: UsageStats | null = null;
let detailBoxVisible: boolean = false;
let fadeTimeout: ReturnType<typeof setTimeout> | null = null;
let isMouseOverBadge: boolean = false;
let isMouseOverDetailBox: boolean = false;
let currentView: 'week' | 'month' = 'week';
let timeSeriesData: TimeSeriesDataPoint[] | null = null;

// Create badge element
export function createUsageBadge(): HTMLDivElement {
    const badge = document.createElement('div');
    badge.id = 'usage-badge';
    badge.className = 'usage-badge';
    badge.textContent = '$0.00';
    badge.title = 'Click for cost chart';

    // Show on click
    badge.addEventListener('click', showDetailBox);

    // Track mouse over badge
    badge.addEventListener('mouseenter', () => {
        isMouseOverBadge = true;
        cancelFadeOut();
    });

    badge.addEventListener('mouseleave', () => {
        isMouseOverBadge = false;
        scheduleFadeOut();
    });

    document.body.appendChild(badge);

    return badge;
}

// Update badge with new stats
export function updateUsageBadge(stats: UsageStats): void {
    currentStats = stats;

    const badge = document.getElementById('usage-badge');
    if (!badge) return;

    // If detail box is not visible, show 24h cost
    // Otherwise, renderChart() will update with week/month total
    if (!detailBoxVisible) {
        badge.textContent = `$${stats.total_cost.toFixed(2)}`;
    }

    // Fetch time-series data and update chart if visible
    if (detailBoxVisible) {
        fetchTimeSeriesData().then(() => {
            renderChart();
        });
    }
}

// Fetch time-series data for charting
async function fetchTimeSeriesData(): Promise<void> {
    try {
        const days = currentView === 'week' ? 7 : 30;
        const { apiFetch } = await import('./api.ts');
        const response = await apiFetch(`/api/timeseries/usage?days=${days}`);
        if (response.ok) {
            timeSeriesData = await response.json() as TimeSeriesDataPoint[];
        }
    } catch (err) {
        console.warn('Failed to fetch time-series data:', err);
        timeSeriesData = [];
    }
}

// Show detail box with chart
async function showDetailBox(): Promise<void> {
    let detailBox = document.getElementById('usage-detail-box');

    if (!detailBox) {
        detailBox = createDetailBox();
    }

    // Reset to week view
    currentView = 'week';
    detailBox.classList.remove('expanded');

    // Show immediately (before data fetch)
    detailBox.classList.remove('fading');
    detailBox.classList.add('visible');
    detailBoxVisible = true;

    // Cancel any pending fade
    cancelFadeOut();

    // Fetch data and render chart (async, doesn't block display)
    fetchTimeSeriesData().then(() => {
        renderChart();
    });
}

// Hide detail box with fade-out
function hideDetailBox(): void {
    const detailBox = document.getElementById('usage-detail-box');
    if (!detailBox) return;

    detailBox.classList.add('fading');

    // Wait for fade animation to complete before hiding
    setTimeout(() => {
        detailBox.classList.remove('visible');
        detailBoxVisible = false;

        // Restore badge to 24h cost
        if (currentStats) {
            const badge = document.getElementById('usage-badge');
            if (badge) {
                badge.textContent = `$${currentStats.total_cost.toFixed(2)}`;
            }
        }
    }, 300); // Match CSS transition duration
}

// Schedule fade-out when mouse leaves
function scheduleFadeOut(): void {
    // Only fade out if mouse is not over badge or detail box
    cancelFadeOut();
    fadeTimeout = setTimeout(() => {
        if (!isMouseOverBadge && !isMouseOverDetailBox) {
            hideDetailBox();
        }
    }, 500); // 500ms delay before fading
}

// Cancel scheduled fade-out
function cancelFadeOut(): void {
    if (fadeTimeout) {
        clearTimeout(fadeTimeout);
        fadeTimeout = null;
    }
}

// Create detail box structure
function createDetailBox(): HTMLDivElement {
    const detailBox = document.createElement('div');
    detailBox.id = 'usage-detail-box';
    detailBox.className = 'usage-detail-box';
    detailBox.setAttribute('role', 'status');
    detailBox.setAttribute('aria-label', 'Cost Usage Chart');

    // Create chart container
    const chartContainer = document.createElement('div');
    chartContainer.className = 'usage-chart-container';
    chartContainer.id = 'usage-chart-container';
    detailBox.appendChild(chartContainer);

    // Click to expand to month view
    detailBox.addEventListener('click', () => {
        if (currentView === 'week') {
            expandToMonthView();
        }
    });

    // Track mouse over detail box
    detailBox.addEventListener('mouseenter', () => {
        isMouseOverDetailBox = true;
        cancelFadeOut();
    });

    detailBox.addEventListener('mouseleave', () => {
        isMouseOverDetailBox = false;
        scheduleFadeOut();
    });

    document.body.appendChild(detailBox);

    return detailBox;
}

// Expand to month view
async function expandToMonthView(): Promise<void> {
    const detailBox = document.getElementById('usage-detail-box');
    if (!detailBox) return;

    currentView = 'month';
    detailBox.classList.add('expanded');

    // Fetch month data and re-render (async, doesn't block expansion)
    fetchTimeSeriesData().then(() => {
        renderChart();
    });
}

// Type definitions for D3 data
interface ChartDataPoint {
    date: Date | null;
    cost: number;
    requests: number;
}

// Render chart using D3
// TODO: Research better time-series charting - see GitHub issue for WebSocket streaming investigation
function renderChart(): void {
    const container = document.getElementById('usage-chart-container');
    if (!container || !timeSeriesData || timeSeriesData.length === 0) return;

    // Update badge with total cost for current view
    const totalCost = timeSeriesData.reduce((sum: number, d: TimeSeriesDataPoint) => sum + d.cost, 0);
    const badge = document.getElementById('usage-badge');
    if (badge) {
        badge.textContent = `$${totalCost.toFixed(2)}`;
    }

    // Clear previous chart
    container.innerHTML = '';

    const width = container.clientWidth;
    const height = container.clientHeight;
    const margin = { top: 10, right: 10, bottom: 20, left: 40 };
    const chartWidth = width - margin.left - margin.right;
    const chartHeight = height - margin.top - margin.bottom;

    // Create SVG
    const svg = d3.select(container)
        .append('svg')
        .attr('class', 'usage-chart-svg')
        .attr('width', width)
        .attr('height', height);

    const g = svg.append('g')
        .attr('transform', `translate(${margin.left},${margin.top})`);

    // Parse dates
    const parseTime = d3.timeParse('%Y-%m-%d');
    const data: ChartDataPoint[] = timeSeriesData.map(d => ({
        date: parseTime(d.date),
        cost: d.cost,
        requests: d.requests
    }));

    // Filter out any null dates
    const validData = data.filter(d => d.date !== null) as Array<{ date: Date; cost: number; requests: number }>;

    // Scales
    const x = d3.scaleTime()
        .domain(d3.extent(validData, d => d.date) as [Date, Date])
        .range([0, chartWidth]);

    const y0 = d3.scaleLinear()
        .domain([0, (d3.max(validData, d => d.cost) || 0) * 1.1])
        .range([chartHeight, 0]);

    const y1 = d3.scaleLinear()
        .domain([0, (d3.max(validData, d => d.requests) || 0) * 1.1])
        .range([chartHeight, 0]);

    // Area generator for cost
    const area = d3.area<{ date: Date; cost: number; requests: number }>()
        .x(d => x(d.date))
        .y0(chartHeight)
        .y1(d => y0(d.cost))
        .curve(d3.curveMonotoneX);

    // Line generator for requests
    const line = d3.line<{ date: Date; cost: number; requests: number }>()
        .x(d => x(d.date))
        .y(d => y1(d.requests))
        .curve(d3.curveMonotoneX);

    // Draw cost area
    g.append('path')
        .datum(validData)
        .attr('class', 'usage-cost-area')
        .attr('d', area);

    // Draw requests line
    g.append('path')
        .datum(validData)
        .attr('class', 'usage-requests-line')
        .attr('d', line);

    // X axis
    g.append('g')
        .attr('class', 'usage-chart-axis')
        .attr('transform', `translate(0,${chartHeight})`)
        .call(d3.axisBottom(x).ticks(currentView === 'week' ? 7 : 10).tickFormat(d3.timeFormat('%m/%d') as any));

    // Y axis (cost)
    g.append('g')
        .attr('class', 'usage-chart-axis')
        .call(d3.axisLeft(y0).ticks(5).tickFormat(d => `$${(+d).toFixed(2)}`));
}

// Initialize usage badge
export function initUsageBadge(): void {
    createUsageBadge();
}

// Handle usage update from WebSocket
export function handleUsageUpdate(data: UsageStats): void {
    updateUsageBadge(data);
}