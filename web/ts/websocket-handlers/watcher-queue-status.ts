/**
 * Watcher Queue Status Handler
 *
 * Two visual layers driven by watcher_queue_status broadcasts:
 * 1. Animated dot particles drifting along element borders (queue activity)
 * 2. Metadata pill on title bar hover (queue + execution stats popover)
 */

import { log, SEG } from '../logger';
import { formatRelativeTimeUnix } from '../html-utils';
import type { WatcherQueueStatusMessage } from '../../types/websocket';
import type { WatcherBroadcastStats } from '../generated/proto/plugin/grpc/protocol/server';

// ── Constants ────────────────────────────────────────────────────────

const ORBIT_MIN_S = 6;
const ORBIT_MAX_S = 10;
const FADE_OUT_MS = 500;
const MAX_PARTICLES = 8;

// Cached execution stats survive across broadcasts so pills remain visible
// after an element's queue drains. Only queueCount resets to 0.
const statsCache = new Map<string, ElementQueueData>();

// ── Aggregated per-element data ────────────────────────────────────────

interface ElementQueueData {
    queueCount: number;
    fireCount: number;
    errorCount: number;
    lastFiredAt: number; // Unix seconds, 0 = never
    lastError: string;
}

// ── Element ID resolution ─────────────────────────────────────────────

function resolveElementId(
    watcherId: string,
    targetElements: Record<string, string> | undefined,
): string | null {
    if (watcherId.startsWith('ax-element-')) {
        return watcherId.substring('ax-element-'.length);
    }
    if (watcherId.startsWith('se-element-')) {
        return watcherId.substring('se-element-'.length);
    }
    if (watcherId.startsWith('meld-edge-') && targetElements) {
        return targetElements[watcherId] || null;
    }
    return null;
}

/**
 * Aggregate per-watcher data into per-element data.
 * Multiple watchers can target the same element (e.g. two meld edges → same py element).
 */
function aggregatePerElement(data: WatcherQueueStatusMessage): Map<string, ElementQueueData> {
    const perElement = new Map<string, ElementQueueData>();

    for (const [watcherId, count] of Object.entries(data.per_watcher)) {
        const itemId = resolveElementId(watcherId, data.target_elements);
        if (!itemId) continue;

        const existing = perElement.get(itemId);
        const stats: WatcherBroadcastStats | undefined = data.watcher_stats?.[watcherId];

        if (existing) {
            existing.queueCount += count;
            existing.fireCount += stats?.fire_count ?? 0;
            existing.errorCount += stats?.error_count ?? 0;
            // Keep most recent fire time
            const firedAt = stats?.last_fired_at ?? 0;
            if (firedAt > existing.lastFiredAt) {
                existing.lastFiredAt = firedAt;
            }
            // Keep most recent error
            if (stats?.last_error && (!existing.lastError || firedAt > existing.lastFiredAt)) {
                existing.lastError = stats.last_error;
            }
        } else {
            perElement.set(itemId, {
                queueCount: count,
                fireCount: stats?.fire_count ?? 0,
                errorCount: stats?.error_count ?? 0,
                lastFiredAt: stats?.last_fired_at ?? 0,
                lastError: stats?.last_error ?? '',
            });
        }
    }

    return perElement;
}

// ── Particles ────────────────────────────────────────────────────────

function ensureParticleContainer(elementEl: HTMLElement): HTMLElement {
    let container = elementEl.querySelector('.queue-particles') as HTMLElement | null;
    if (!container) {
        container = document.createElement('div');
        container.className = 'queue-particles';
        elementEl.appendChild(container);
    }
    return container;
}

function createParticle(index: number): HTMLElement {
    const dot = document.createElement('div');
    dot.className = 'queue-particle';
    const orbitDuration = ORBIT_MIN_S + Math.random() * (ORBIT_MAX_S - ORBIT_MIN_S);
    const delay = -(Math.random() * orbitDuration);
    dot.style.setProperty('--orbit-duration', `${orbitDuration.toFixed(1)}s`);
    dot.style.setProperty('--i', String(index));
    dot.style.animationDelay = `${delay.toFixed(2)}s, ${delay.toFixed(2)}s`;
    return dot;
}

function fadeOutElement(el: HTMLElement, removeDelay = FADE_OUT_MS): void {
    el.classList.add('fading');
    setTimeout(() => el.remove(), removeDelay);
}

function updateParticles(elementEl: HTMLElement, queueCount: number): void {
    const container = ensureParticleContainer(elementEl);
    const targetCount = Math.min(Math.ceil(queueCount / 2), MAX_PARTICLES);
    const current = container.querySelectorAll('.queue-particle:not(.fading)');

    if (current.length < targetCount) {
        for (let i = current.length; i < targetCount; i++) {
            container.appendChild(createParticle(i));
        }
    } else if (current.length > targetCount) {
        for (let i = current.length - 1; i >= targetCount; i--) {
            fadeOutElement(current[i] as HTMLElement);
        }
    }
}

function clearParticles(container: Element): void {
    for (const p of container.querySelectorAll('.queue-particle:not(.fading)')) {
        fadeOutElement(p as HTMLElement);
    }
    setTimeout(() => container.remove(), FADE_OUT_MS);
}

// ── Metadata pill ────────────────────────────────────────────────────

function buildPopoverContent(d: ElementQueueData): string {
    const lines: string[] = [];
    lines.push(`queued: ${d.queueCount}`);
    lines.push(`fired: ${d.fireCount}`);

    if (d.errorCount > 0) {
        lines.push(`<span style="color: #d45030">errors: ${d.errorCount}</span>`);
    } else {
        lines.push(`errors: 0`);
    }

    lines.push(`last fired: ${formatRelativeTimeUnix(d.lastFiredAt)}`);

    if (d.lastError) {
        const truncated = d.lastError.length > 80
            ? d.lastError.substring(0, 80) + '...'
            : d.lastError;
        lines.push(`<span style="color: #d45030">last error: ${truncated}</span>`);
    }

    return lines.join('\n');
}

function ensureMetaPill(elementEl: HTMLElement): HTMLElement | null {
    // Skip attestation elements — they have their own .as-meta-pill
    if (elementEl.querySelector('.as-meta-pill')) return null;

    let pill = elementEl.querySelector('.element-meta-pill') as HTMLElement | null;
    if (pill) return pill;

    // Find the title bar — pill is positioned relative to it
    const titleBar = elementEl.querySelector('.title-bar') as HTMLElement | null;
    if (!titleBar) return null;

    // Title bar becomes the positioning context (matches attestation element's wrapper pattern)
    if (getComputedStyle(titleBar).position === 'static') {
        titleBar.style.position = 'relative';
    }

    pill = document.createElement('div');
    pill.className = 'element-meta-pill';

    const popover = document.createElement('div');
    popover.className = 'meta-popover element-meta-popover';
    pill.appendChild(popover);

    // Append inside title bar so bottom: -4px hangs off the title bar, not the whole element
    titleBar.appendChild(pill);
    return pill;
}

function updateMetaPill(elementEl: HTMLElement, d: ElementQueueData): void {
    const pill = ensureMetaPill(elementEl);
    if (!pill) return;

    const popover = pill.querySelector('.element-meta-popover') as HTMLElement | null;
    if (popover) {
        popover.innerHTML = buildPopoverContent(d);
    }
}

// ── Main handler ─────────────────────────────────────────────────────

export function handleWatcherQueueStatus(data: WatcherQueueStatusMessage): void {
    log.debug(SEG.WS, 'Watcher queue status:', data.total_queued, 'queued');

    const perElement = aggregatePerElement(data);

    // Merge current broadcast into cache
    for (const [itemId, elementData] of perElement) {
        statsCache.set(itemId, elementData);
    }

    // Zero out queueCount for cached elements absent from this broadcast
    for (const [itemId, cached] of statsCache) {
        if (!perElement.has(itemId)) {
            cached.queueCount = 0;
        }
    }

    // Update visuals from cache
    for (const [itemId, cached] of statsCache) {
        const elementEl = document.querySelector(`[data-element-id="${CSS.escape(itemId)}"]`) as HTMLElement | null;
        if (!elementEl) {
            // Element removed from DOM — drop from cache
            statsCache.delete(itemId);
            continue;
        }

        // Particles only while items are queued
        if (cached.queueCount > 0) {
            updateParticles(elementEl, cached.queueCount);
        }

        // Pill always shows cached stats
        updateMetaPill(elementEl, cached);
    }

    // Clear particles for elements whose queue has drained
    for (const container of document.querySelectorAll('.queue-particles')) {
        const elementEl = container.closest('[data-element-id]') as HTMLElement | null;
        if (!elementEl) {
            container.remove();
            continue;
        }
        const itemId = elementEl.dataset.elementId;
        if (itemId) {
            const cached = statsCache.get(itemId);
            if (!cached || cached.queueCount === 0) {
                clearParticles(container);
            }
        }
    }
}
