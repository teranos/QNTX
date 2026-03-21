/**
 * Eviction Chart — D3 bar chart showing eviction history over time.
 * Used by the database glyph to visualize bounded storage enforcement.
 */

import * as d3 from 'd3';

export interface EvictionRecord {
    event_type: string;
    actor: string;
    context: string;
    entity: string;
    deletions_count: number;
    message: string;
    timestamp: number;
}

const MAX_EVICTIONS = 1000;
const evictions: EvictionRecord[] = [];

/** Seed eviction history from backend response (called once on glyph open). */
export function seedEvictions(records: Array<{ event_type: string; actor: string; context: string; entity: string; deletions_count: number; message: string; timestamp: string }>): void {
    if (evictions.length > 0 || !records || records.length === 0) return;
    for (const ev of records) {
        evictions.push({
            event_type: ev.event_type,
            actor: ev.actor,
            context: ev.context,
            entity: ev.entity,
            deletions_count: ev.deletions_count,
            message: ev.message,
            timestamp: new Date(ev.timestamp).getTime(),
        });
    }
}

/** Record a live eviction from WebSocket. */
export function recordEviction(data: { event_type: string; actor: string; context: string; entity: string; deletions_count: number; message: string }): void {
    evictions.unshift({
        ...data,
        timestamp: Date.now(),
    });
    if (evictions.length > MAX_EVICTIONS) {
        evictions.length = MAX_EVICTIONS;
    }
}

/** Get summary stats for the eviction header row. */
export function getEvictionSummary(): { count: number; totalEvicted: number } {
    return {
        count: evictions.length,
        totalEvicted: evictions.reduce((sum, e) => sum + e.deletions_count, 0),
    };
}

/** Returns true if there are evictions to display. */
export function hasEvictions(): boolean {
    return evictions.length > 0;
}

/** Render the eviction bar chart into the given container element. */
export function renderEvictionChart(container: HTMLElement): void {
    container.innerHTML = '';

    if (evictions.length === 0) return;

    // Aggregate evictions into hourly buckets
    const buckets = new Map<number, number>();
    for (const ev of evictions) {
        const hour = Math.floor(ev.timestamp / 3600000) * 3600000;
        buckets.set(hour, (buckets.get(hour) ?? 0) + ev.deletions_count);
    }

    const data = Array.from(buckets.entries())
        .map(([time, count]) => ({ time: new Date(time), count }))
        .sort((a, b) => a.time.getTime() - b.time.getTime());

    if (data.length === 0) return;

    const width = 360;
    const height = 80;
    const margin = { top: 8, right: 8, bottom: 20, left: 30 };
    const innerW = width - margin.left - margin.right;
    const innerH = height - margin.top - margin.bottom;

    const svg = d3.select(container)
        .append('svg')
        .attr('viewBox', `0 0 ${width} ${height}`)
        .style('width', '100%')
        .style('height', 'auto')
        .style('border-radius', '4px');

    const g = svg.append('g').attr('transform', `translate(${margin.left},${margin.top})`);

    const barWidth = Math.max(4, Math.min(20, innerW / data.length - 2));

    const xScale = d3.scaleTime()
        .domain(d3.extent(data, d => d.time) as [Date, Date])
        .range([barWidth / 2, innerW - barWidth / 2]);

    const yScale = d3.scaleLinear()
        .domain([0, d3.max(data, d => d.count) ?? 1])
        .nice()
        .range([innerH, 0]);

    // Bars
    g.selectAll('rect')
        .data(data)
        .enter()
        .append('rect')
        .attr('x', d => xScale(d.time) - barWidth / 2)
        .attr('y', d => yScale(d.count))
        .attr('width', barWidth)
        .attr('height', d => innerH - yScale(d.count))
        .attr('fill', '#ef4444')
        .attr('opacity', 0.7)
        .attr('rx', 1);

    // X axis
    g.append('g')
        .attr('transform', `translate(0,${innerH})`)
        .call(d3.axisBottom(xScale).ticks(4).tickFormat(d => d3.timeFormat('%H:%M')(d as Date)))
        .selectAll('text')
        .attr('fill', '#94a3b8')
        .style('font-size', '9px');

    // Y axis
    g.append('g')
        .call(d3.axisLeft(yScale).ticks(3).tickFormat(d3.format('d')))
        .selectAll('text')
        .attr('fill', '#94a3b8')
        .style('font-size', '9px');

    // Style axis lines
    svg.selectAll('.domain, .tick line').attr('stroke', '#334155');
}
