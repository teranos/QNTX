/**
 * Watcher Queue Status Handler
 *
 * Two visual layers driven by watcher_queue_status broadcasts:
 * 1. Animated dot particles drifting along glyph borders (queue activity)
 * 2. Metadata pill on title bar hover (queue + execution stats popover)
 */

import { log, SEG } from '../logger';
import type { WatcherQueueStatusMessage } from '../../types/websocket';
import type { WatcherBroadcastStats } from '../../../types/generated/typescript/server';

// ── Constants ────────────────────────────────────────────────────────

const ORBIT_MIN_S = 6;
const ORBIT_MAX_S = 10;
const FADE_OUT_MS = 500;
const MAX_PARTICLES = 8;

// Cached execution stats survive across broadcasts so pills remain visible
// after a glyph's queue drains. Only queueCount resets to 0.
const statsCache = new Map<string, GlyphQueueData>();

// ── Aggregated per-glyph data ────────────────────────────────────────

interface GlyphQueueData {
    queueCount: number;
    fireCount: number;
    errorCount: number;
    lastFiredAt: number; // Unix seconds, 0 = never
    lastError: string;
}

// ── Glyph ID resolution ─────────────────────────────────────────────

function resolveGlyphId(
    watcherId: string,
    targetGlyphs: Record<string, string> | undefined,
): string | null {
    if (watcherId.startsWith('ax-glyph-')) {
        return watcherId.substring('ax-glyph-'.length);
    }
    if (watcherId.startsWith('se-glyph-')) {
        return watcherId.substring('se-glyph-'.length);
    }
    if (watcherId.startsWith('meld-edge-') && targetGlyphs) {
        return targetGlyphs[watcherId] || null;
    }
    return null;
}

/**
 * Aggregate per-watcher data into per-glyph data.
 * Multiple watchers can target the same glyph (e.g. two meld edges → same py glyph).
 */
function aggregatePerGlyph(data: WatcherQueueStatusMessage): Map<string, GlyphQueueData> {
    const perGlyph = new Map<string, GlyphQueueData>();

    for (const [watcherId, count] of Object.entries(data.per_watcher)) {
        const glyphId = resolveGlyphId(watcherId, data.target_glyphs);
        if (!glyphId) continue;

        const existing = perGlyph.get(glyphId);
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
            perGlyph.set(glyphId, {
                queueCount: count,
                fireCount: stats?.fire_count ?? 0,
                errorCount: stats?.error_count ?? 0,
                lastFiredAt: stats?.last_fired_at ?? 0,
                lastError: stats?.last_error ?? '',
            });
        }
    }

    return perGlyph;
}

// ── Particles ────────────────────────────────────────────────────────

function ensureParticleContainer(glyphEl: HTMLElement): HTMLElement {
    let container = glyphEl.querySelector('.queue-particles') as HTMLElement | null;
    if (!container) {
        container = document.createElement('div');
        container.className = 'queue-particles';
        glyphEl.appendChild(container);
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

function updateParticles(glyphEl: HTMLElement, queueCount: number): void {
    const container = ensureParticleContainer(glyphEl);
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

function relativeTime(unixSeconds: number): string {
    if (!unixSeconds) return 'never';
    const delta = Math.floor(Date.now() / 1000) - unixSeconds;
    if (delta < 0) return 'just now';
    if (delta < 60) return `${delta}s ago`;
    if (delta < 3600) return `${Math.floor(delta / 60)}m ago`;
    if (delta < 86400) return `${Math.floor(delta / 3600)}h ago`;
    return `${Math.floor(delta / 86400)}d ago`;
}

function buildPopoverContent(d: GlyphQueueData): string {
    const lines: string[] = [];
    lines.push(`queued: ${d.queueCount}`);
    lines.push(`fired: ${d.fireCount}`);

    if (d.errorCount > 0) {
        lines.push(`<span style="color: #d45030">errors: ${d.errorCount}</span>`);
    } else {
        lines.push(`errors: 0`);
    }

    lines.push(`last fired: ${relativeTime(d.lastFiredAt)}`);

    if (d.lastError) {
        const truncated = d.lastError.length > 80
            ? d.lastError.substring(0, 80) + '...'
            : d.lastError;
        lines.push(`<span style="color: #d45030">last error: ${truncated}</span>`);
    }

    return lines.join('\n');
}

function ensureMetaPill(glyphEl: HTMLElement): HTMLElement | null {
    // Skip attestation glyphs — they have their own .as-meta-pill
    if (glyphEl.querySelector('.as-meta-pill')) return null;

    let pill = glyphEl.querySelector('.glyph-meta-pill') as HTMLElement | null;
    if (pill) return pill;

    // Find the title bar — pill is positioned relative to it
    const titleBar = glyphEl.querySelector('.glyph-title-bar') as HTMLElement | null;
    if (!titleBar) return null;

    // Title bar becomes the positioning context (matches attestation glyph's wrapper pattern)
    if (getComputedStyle(titleBar).position === 'static') {
        titleBar.style.position = 'relative';
    }

    pill = document.createElement('div');
    pill.className = 'glyph-meta-pill';

    const popover = document.createElement('div');
    popover.className = 'glyph-meta-popover';
    pill.appendChild(popover);

    // Append inside title bar so bottom: -4px hangs off the title bar, not the whole glyph
    titleBar.appendChild(pill);
    return pill;
}

function updateMetaPill(glyphEl: HTMLElement, d: GlyphQueueData): void {
    const pill = ensureMetaPill(glyphEl);
    if (!pill) return;

    const popover = pill.querySelector('.glyph-meta-popover') as HTMLElement | null;
    if (popover) {
        popover.innerHTML = buildPopoverContent(d);
    }
}

// ── Main handler ─────────────────────────────────────────────────────

export function handleWatcherQueueStatus(data: WatcherQueueStatusMessage): void {
    log.debug(SEG.WS, 'Watcher queue status:', data.total_queued, 'queued');

    const perGlyph = aggregatePerGlyph(data);

    // Merge current broadcast into cache
    for (const [glyphId, glyphData] of perGlyph) {
        statsCache.set(glyphId, glyphData);
    }

    // Zero out queueCount for cached glyphs absent from this broadcast
    for (const [glyphId, cached] of statsCache) {
        if (!perGlyph.has(glyphId)) {
            cached.queueCount = 0;
        }
    }

    // Update visuals from cache
    for (const [glyphId, cached] of statsCache) {
        const glyphEl = document.querySelector(`[data-glyph-id="${CSS.escape(glyphId)}"]`) as HTMLElement | null;
        if (!glyphEl) {
            // Glyph removed from DOM — drop from cache
            statsCache.delete(glyphId);
            continue;
        }

        // Particles only while items are queued
        if (cached.queueCount > 0) {
            updateParticles(glyphEl, cached.queueCount);
        }

        // Pill always shows cached stats
        updateMetaPill(glyphEl, cached);
    }

    // Clear particles for glyphs whose queue has drained
    for (const container of document.querySelectorAll('.queue-particles')) {
        const glyphEl = container.closest('[data-glyph-id]') as HTMLElement | null;
        if (!glyphEl) {
            container.remove();
            continue;
        }
        const glyphId = glyphEl.dataset.glyphId;
        if (glyphId) {
            const cached = statsCache.get(glyphId);
            if (!cached || cached.queueCount === 0) {
                clearParticles(container);
            }
        }
    }
}
