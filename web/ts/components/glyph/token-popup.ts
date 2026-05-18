/**
 * Token Popup — hover overlay for stream glyph tokens.
 *
 * Single-column view: final candidates with origin markers showing where
 * each word started (raw probability) vs where it ended up. The color
 * of the shift indicates which sampler stage had the biggest effect.
 *
 * Interactive: mouse can enter the popup. Lingering expands a color legend.
 */

import type { LLMTokenCandidate, SamplerStageSignal } from '@generated/server';
import { el } from '../../html-utils';

export interface TokenPopup {
    show(span: HTMLSpanElement): void;
    hide(): void;
    /** Call on mouseleave from the token span — starts a delayed hide */
    scheduleHide(): void;
    /** Call to cancel a pending hide (e.g. mouse entered the popup) */
    cancelHide(): void;
    destroy(): void;
}

/** Escape whitespace characters for display */
function escapeToken(text: string): string {
    let out = '';
    for (let i = 0; i < text.length; i++) {
        const ch = text[i];
        if (ch === '\n') { out += '\\n'; }
        else if (ch === '\t') { out += '\\t'; }
        else if (ch === '\r') { out += '\\r'; }
        else { out += ch; }
    }
    return out;
}

// Stage colors — each sampler gets a distinct hue
const STAGE_COLORS: Record<string, string> = {
    penalties: 'hsla(0, 70%, 55%, 0.8)',     // red — suppression
    top_k:     'hsla(210, 60%, 55%, 0.6)',   // blue — filtering
    top_p:     'hsla(210, 60%, 55%, 0.6)',   // blue — filtering
    min_p:     'hsla(270, 50%, 55%, 0.6)',   // purple — filtering
    typical:   'hsla(270, 50%, 55%, 0.6)',   // purple — filtering
    temp:      'hsla(40, 80%, 55%, 0.8)',    // amber — sharpening
};

const STAGE_LABELS: Record<string, string> = {
    penalties: 'repetition penalty',
    top_k:     'top-k filter',
    top_p:     'nucleus filter',
    min_p:     'min-p filter',
    typical:   'typical filter',
    temp:      'temperature',
};

const LEGEND_ITEMS = [
    { color: STAGE_COLORS.temp, label: 'temperature sharpened' },
    { color: STAGE_COLORS.penalties, label: 'repetition penalty' },
    { color: STAGE_COLORS.top_k, label: 'top-k / nucleus filtered' },
    { color: STAGE_COLORS.min_p, label: 'min-p / typical filtered' },
];

interface StageTrace {
    name: string;
    prob: number;
    delta: number;
}

/**
 * For each candidate in the final stage, trace back through earlier stages
 * to find the raw (first) probability. Also find which stage caused the
 * biggest shift for this token.
 */
function traceCandidate(
    tokenId: number,
    stages: SamplerStageSignal[],
): { rawProb: number; biggestStage: string } {
    let rawProb = 0;
    let biggestStage = '';
    let biggestDelta = 0;
    let prevProb = 0;

    for (const stage of stages) {
        if (!stage.top_k) continue;
        const match = stage.top_k.find(c => c.id === tokenId);
        const prob = match ? match.prob : 0;

        if (stage.name === 'logits') {
            rawProb = prob;
            prevProb = prob;
            continue;
        }

        const delta = Math.abs(prob - prevProb);
        if (delta > biggestDelta) {
            biggestDelta = delta;
            biggestStage = stage.name;
        }
        prevProb = prob;
    }

    return { rawProb, biggestStage };
}

/** Full per-stage trace for a token — used in the detail sub-popover */
function fullTrace(tokenId: number, stages: SamplerStageSignal[]): StageTrace[] {
    const result: StageTrace[] = [];
    let prevProb = 0;

    for (const stage of stages) {
        if (!stage.top_k) continue;
        const match = stage.top_k.find(c => c.id === tokenId);
        const prob = match ? match.prob : 0;
        const delta = stage.name === 'logits' ? 0 : prob - prevProb;
        result.push({ name: stage.name, prob, delta });
        prevProb = prob;
    }

    return result;
}

/**
 * Return a background color for the token based on which sampler stage
 * had the biggest effect on the chosen token. Alpha scales with how
 * much the probability shifted — subtle when samplers barely changed
 * anything, vivid when they made a big difference.
 */
export function samplerInfluenceColor(stages: SamplerStageSignal[]): string {
    if (stages.length === 0) return 'transparent';

    // Find the chosen token (highest prob in final stage)
    const finalStage = stages[stages.length - 1];
    if (!finalStage.top_k || finalStage.top_k.length === 0) return 'transparent';

    let chosenId = finalStage.top_k[0].id;
    let chosenProb = finalStage.top_k[0].prob;
    for (const c of finalStage.top_k) {
        if (c.prob > chosenProb) {
            chosenId = c.id;
            chosenProb = c.prob;
        }
    }

    const trace = traceCandidate(chosenId, stages);
    const delta = Math.abs(chosenProb - trace.rawProb);

    if (delta < 0.01) return 'transparent';

    // Map the stage to a hue
    const stageHues: Record<string, number> = {
        penalties: 0,
        top_k: 210,
        top_p: 210,
        min_p: 270,
        typical: 270,
        temp: 40,
    };
    const hue = stageHues[trace.biggestStage] ?? 30;
    // Alpha proportional to the shift magnitude, capped at 0.45
    const alpha = Math.min(0.45, delta * 1.5);
    if (alpha < 0.02) return 'transparent';
    return `hsla(${hue}, 70%, 50%, ${alpha.toFixed(3)})`;
}

export function createTokenPopup(): TokenPopup {
    const popupEl = el('div', { class: 'token-popup', style: { display: 'none' } });
    document.body.appendChild(popupEl);

    let hideTimer: ReturnType<typeof setTimeout> | null = null;
    let legendTimer: ReturnType<typeof setTimeout> | null = null;
    let legendVisible = false;
    let legendEl: HTMLDivElement | null = null;

    // Nested detail sub-popover — interactive, shows full stage-by-stage trace
    const detailEl = el('div', { class: 'token-popup-detail', style: { display: 'none' } });
    document.body.appendChild(detailEl);

    let detailHideTimer: ReturnType<typeof setTimeout> | null = null;

    detailEl.addEventListener('mouseenter', () => {
        if (detailHideTimer) { clearTimeout(detailHideTimer); detailHideTimer = null; }
        // Keep parent popup alive while mouse is in the detail popover
        if (hideTimer) { clearTimeout(hideTimer); hideTimer = null; }
    });
    detailEl.addEventListener('mouseleave', () => {
        detailHideTimer = setTimeout(hideDetail, 120);
        // Schedule parent popup hide too
        if (!hideTimer) {
            hideTimer = setTimeout(() => {
                hideDetail();
                popupEl.style.display = 'none';
            }, 150);
        }
    });

    function positionDetail(target: HTMLElement) {
        const r = target.getBoundingClientRect();
        detailEl.style.display = '';
        let left = r.right + 6;
        let top = r.top;
        const dw = detailEl.offsetWidth;
        const dh = detailEl.offsetHeight;
        if (left + dw > window.innerWidth - 4) {
            left = r.left - dw - 6;
        }
        if (left < 4) left = 4;
        if (top + dh > window.innerHeight - 4) {
            top = window.innerHeight - dh - 4;
        }
        if (top < 4) top = 4;
        detailEl.style.left = `${left}px`;
        detailEl.style.top = `${top}px`;
    }

    function showStageDetail(target: HTMLElement, tokenId: number, tokenText: string, stages: SamplerStageSignal[]) {
        if (detailHideTimer) { clearTimeout(detailHideTimer); detailHideTimer = null; }
        detailEl.innerHTML = '';

        const header = el('div', { class: 'token-popup-detail-header', text: escapeToken(tokenText) });
        detailEl.appendChild(header);

        const traces = fullTrace(tokenId, stages);

        for (const t of traces) {
            const color = t.name === 'logits'
                ? 'rgba(255,255,255,0.3)'
                : (STAGE_COLORS[t.name] ?? 'rgba(255,255,255,0.3)');
            const label = t.name === 'logits'
                ? 'raw logits'
                : (STAGE_LABELS[t.name] ?? t.name);

            const dot = el('span', { class: 'token-popup-detail-dot', style: { background: color } });
            const nameEl = el('span', { class: 'token-popup-detail-name', text: label });
            const probEl = el('span', { class: 'token-popup-detail-prob', text: `${(t.prob * 100).toFixed(1)}%` });
            const row = el('div', { class: 'token-popup-detail-row' }, [dot, nameEl, probEl]);

            if (t.name !== 'logits' && Math.abs(t.delta) > 0.001) {
                const sign = t.delta > 0 ? '+' : '';
                row.appendChild(el('span', {
                    class: 'token-popup-detail-delta',
                    text: `${sign}${(t.delta * 100).toFixed(1)}`,
                    style: { color: t.delta > 0 ? 'hsla(120, 60%, 60%, 0.9)' : 'hsla(0, 60%, 60%, 0.9)' },
                }));
            }

            detailEl.appendChild(row);
        }

        positionDetail(target);
    }

    function scheduleDetailHide() {
        if (detailHideTimer) clearTimeout(detailHideTimer);
        detailHideTimer = setTimeout(hideDetail, 120);
    }

    function hideDetail() {
        if (detailHideTimer) { clearTimeout(detailHideTimer); detailHideTimer = null; }
        detailEl.style.display = 'none';
        detailEl.innerHTML = '';
    }

    function clearTimers() {
        if (hideTimer) { clearTimeout(hideTimer); hideTimer = null; }
        if (legendTimer) { clearTimeout(legendTimer); legendTimer = null; }
    }

    function showLegend() {
        if (legendVisible || !legendEl) return;
        legendVisible = true;
        legendEl.style.display = '';
    }

    function hideLegend() {
        if (!legendVisible || !legendEl) return;
        legendVisible = false;
        legendEl.style.display = 'none';
    }

    // Keep popup alive when mouse enters it, expand legend after 400ms
    popupEl.addEventListener('mouseenter', () => {
        if (hideTimer) { clearTimeout(hideTimer); hideTimer = null; }
        if (legendTimer) clearTimeout(legendTimer);
        legendTimer = setTimeout(showLegend, 400);
    });

    popupEl.addEventListener('mouseleave', () => {
        clearTimers();
        hideLegend();
        scheduleDetailHide();
        hideTimer = setTimeout(() => {
            hideDetail();
            popupEl.style.display = 'none';
        }, 100);
    });

    function show(span: HTMLSpanElement): void {
        clearTimers();
        hideLegend();

        const confidence = span.dataset.confidence;
        if (!confidence) return;

        const topKRaw = span.dataset.topK;
        const stagesRaw = span.dataset.samplerStages;

        let candidates: LLMTokenCandidate[] = [];
        if (topKRaw) {
            try { candidates = JSON.parse(topKRaw); } catch { /* skip */ }
        }

        let stages: SamplerStageSignal[] = [];
        if (stagesRaw) {
            try { stages = JSON.parse(stagesRaw); } catch { /* skip */ }
        }

        popupEl.innerHTML = '';

        const chosenText = span.textContent ?? '';

        // Use the final stage's candidates if available, otherwise pre-sampler
        const finalStage = stages.length > 0
            ? stages[stages.length - 1]
            : null;
        const displayCandidates = finalStage?.top_k ?? candidates;

        if (displayCandidates.length > 0) {
            const list = el('div', { class: 'token-popup-candidates' });

            for (const c of displayCandidates) {
                const row = el('div', { class: 'token-popup-candidate' });
                const isChosen = c.text === chosenText;
                if (isChosen) row.classList.add('token-popup-chosen');

                row.appendChild(el('span', { class: 'token-popup-token-text', text: escapeToken(c.text) }));

                // Bar track with final bar + origin marker
                const barTrack = el('div', { class: 'token-popup-bar-track' });

                const finalPct = c.prob * 100;

                if (stages.length > 0) {
                    const trace = traceCandidate(c.id, stages);
                    const rawPct = trace.rawProb * 100;
                    const color = STAGE_COLORS[trace.biggestStage] ?? 'hsla(30, 100%, 50%, 0.7)';

                    // Final probability bar — colored by biggest contributing stage
                    const bar = el('div', {
                        class: 'token-popup-bar',
                        style: { width: `${finalPct.toFixed(1)}%`, background: color },
                    });
                    bar.addEventListener('mouseenter', () => showStageDetail(bar, c.id, c.text, stages));
                    bar.addEventListener('mouseleave', scheduleDetailHide);
                    barTrack.appendChild(bar);

                    // Origin marker — thin line showing raw probability
                    if (Math.abs(rawPct - finalPct) > 1) {
                        const marker = el('div', {
                            class: 'token-popup-origin',
                            style: { left: `${rawPct.toFixed(1)}%` },
                        });
                        marker.addEventListener('mouseenter', () => showStageDetail(marker, c.id, c.text, stages));
                        marker.addEventListener('mouseleave', scheduleDetailHide);
                        barTrack.appendChild(marker);
                    }
                } else {
                    // No stage data — plain bar
                    barTrack.appendChild(el('div', {
                        class: 'token-popup-bar',
                        style: { width: `${finalPct.toFixed(1)}%` },
                    }));
                }

                row.appendChild(barTrack);

                row.appendChild(el('span', { class: 'token-popup-prob', text: `${finalPct.toFixed(1)}%` }));

                list.appendChild(row);
            }

            popupEl.appendChild(list);
        }

        // Legend — hidden initially, shown after lingering
        legendEl = el('div', { class: 'token-popup-legend', style: { display: 'none' } });
        for (const item of LEGEND_ITEMS) {
            const swatch = el('span', { class: 'token-popup-legend-swatch', style: { background: item.color } });
            const label = el('span', { text: item.label });
            legendEl.appendChild(el('div', { class: 'token-popup-legend-item' }, [swatch, label]));
        }
        // Origin marker explanation
        const originSwatch = el('span', { class: 'token-popup-legend-swatch token-popup-legend-origin' });
        const originLabel = el('span', { text: 'original probability' });
        legendEl.appendChild(el('div', { class: 'token-popup-legend-item' }, [originSwatch, originLabel]));

        popupEl.appendChild(legendEl);

        // Position near the span, viewport-constrained
        popupEl.style.display = '';
        const spanRect = span.getBoundingClientRect();
        const popupWidth = popupEl.offsetWidth;
        const popupHeight = popupEl.offsetHeight;
        const margin = 4;

        let left = spanRect.left;
        let top = spanRect.bottom + margin;

        if (top + popupHeight > window.innerHeight - margin) {
            top = spanRect.top - popupHeight - margin;
        }

        if (left + popupWidth > window.innerWidth - margin) {
            left = window.innerWidth - popupWidth - margin;
        }
        if (left < margin) left = margin;

        popupEl.style.left = `${left}px`;
        popupEl.style.top = `${top}px`;
    }

    function scheduleHide(): void {
        if (hideTimer) clearTimeout(hideTimer);
        hideTimer = setTimeout(() => {
            popupEl.style.display = 'none';
            hideLegend();
        }, 150);
    }

    function cancelHide(): void {
        if (hideTimer) { clearTimeout(hideTimer); hideTimer = null; }
    }

    function hide(): void {
        clearTimers();
        hideLegend();
        hideDetail();
        popupEl.style.display = 'none';
    }

    function destroy(): void {
        clearTimers();
        hideDetail();
        popupEl.remove();
        detailEl.remove();
    }

    return { show, hide, scheduleHide, cancelHide, destroy };
}
