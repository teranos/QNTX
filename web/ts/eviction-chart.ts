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
    predicates?: string[];
    last_seen?: string; // timestamp of oldest evicted attestation
}

const MAX_EVICTIONS = 1000;
const evictions: EvictionRecord[] = [];

/** Seed eviction history from backend response (called once on glyph open). */
export function seedEvictions(records: Array<{ event_type: string; actor: string; context: string; entity: string; deletions_count: number; message: string; timestamp: string; predicates?: string[]; last_seen?: string }>): void {
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
            predicates: ev.predicates,
            last_seen: ev.last_seen,
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

export interface PredicateDetail {
    predicate: string;
    count: number;
    actors: string[];
    contexts: string[];
    entities: string[];
    oldestEvicted: string | null; // timestamp of oldest evicted attestation
    lastEviction: number;         // when the most recent eviction happened (epoch ms)
}

/** Aggregate predicate breakdown with actors, contexts, entities, and data age. */
export function getPredicateBreakdown(): PredicateDetail[] {
    const byPredicate = new Map<string, { count: number; actors: Set<string>; contexts: Set<string>; entities: Set<string>; oldestEvicted: string | null; lastEviction: number }>();

    for (const ev of evictions) {
        if (!ev.predicates || ev.predicates.length === 0) continue;
        for (const pred of ev.predicates) {
            let entry = byPredicate.get(pred);
            if (!entry) {
                entry = { count: 0, actors: new Set(), contexts: new Set(), entities: new Set(), oldestEvicted: null, lastEviction: 0 };
                byPredicate.set(pred, entry);
            }
            entry.count += ev.deletions_count;
            if (ev.actor) entry.actors.add(ev.actor);
            if (ev.context) entry.contexts.add(ev.context);
            if (ev.entity) entry.entities.add(ev.entity);
            if (ev.last_seen && (!entry.oldestEvicted || ev.last_seen < entry.oldestEvicted)) {
                entry.oldestEvicted = ev.last_seen;
            }
            if (ev.timestamp > entry.lastEviction) entry.lastEviction = ev.timestamp;
        }
    }

    return Array.from(byPredicate.entries())
        .map(([predicate, { count, actors, contexts, entities, oldestEvicted, lastEviction }]) => ({
            predicate,
            count,
            actors: [...actors],
            contexts: [...contexts],
            entities: [...entities],
            oldestEvicted,
            lastEviction,
        }))
        .sort((a, b) => b.count - a.count);
}

// Color per enforcement policy
const POLICY_COLORS: Record<string, string> = {
    actor_context_limit: '#ef4444',   // red — per-actor-per-context cap
    actor_contexts_limit: '#f59e0b',  // amber — total contexts per actor
    entity_actors_limit: '#8b5cf6',   // violet — per-entity actor cap
};
const FALLBACK_COLOR = '#64748b'; // slate — unknown policy

function colorForPolicy(eventType: string): string {
    return POLICY_COLORS[eventType] ?? FALLBACK_COLOR;
}

interface StackedBucket {
    time: Date;
    segments: Array<{ eventType: string; count: number }>;
    total: number;
}

/** Render the eviction bar chart into the given container element. */
export function renderEvictionChart(container: HTMLElement): void {
    container.innerHTML = '';

    if (evictions.length === 0) return;

    // Aggregate evictions into hourly buckets, broken down by event_type
    const buckets = new Map<number, Map<string, number>>();
    for (const ev of evictions) {
        const hour = Math.floor(ev.timestamp / 3600000) * 3600000;
        let byType = buckets.get(hour);
        if (!byType) {
            byType = new Map();
            buckets.set(hour, byType);
        }
        byType.set(ev.event_type, (byType.get(ev.event_type) ?? 0) + ev.deletions_count);
    }

    const data: StackedBucket[] = Array.from(buckets.entries())
        .map(([time, byType]) => {
            const segments = Array.from(byType.entries())
                .map(([eventType, count]) => ({ eventType, count }));
            return { time: new Date(time), segments, total: segments.reduce((s, seg) => s + seg.count, 0) };
        })
        .sort((a, b) => a.time.getTime() - b.time.getTime());

    if (data.length === 0) return;

    const width = 360;
    const height = 100;
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
        .domain([0, d3.max(data, d => d.total) ?? 1])
        .nice()
        .range([innerH, 0]);

    // Stacked bars
    for (const bucket of data) {
        const x = xScale(bucket.time) - barWidth / 2;
        let yOffset = innerH;

        for (const seg of bucket.segments) {
            const segHeight = innerH - yScale(seg.count);
            yOffset -= segHeight;

            g.append('rect')
                .attr('x', x)
                .attr('y', yOffset)
                .attr('width', barWidth)
                .attr('height', segHeight)
                .attr('fill', colorForPolicy(seg.eventType))
                .attr('opacity', 0.8)
                .attr('rx', 1);
        }
    }

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

    // Legend
    const policies = [...new Set(evictions.map(e => e.event_type))];
    if (policies.length > 1) {
        const legend = d3.select(container)
            .append('div')
            .style('display', 'flex')
            .style('gap', '12px')
            .style('margin-top', '4px')
            .style('font-size', '10px')
            .style('color', '#94a3b8');

        for (const policy of policies) {
            legend.append('span')
                .html(`<span style="display:inline-block;width:8px;height:8px;border-radius:2px;background:${colorForPolicy(policy)};margin-right:4px;"></span>${policy.split('_').slice(0, -1).join('_')}`);
        }
    }
}
